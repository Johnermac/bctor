package lib

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type FSConfig struct {
	Rootfs        string   // path to prepared rootfs
	ReadOnly  bool
	Proc     bool
	Sys      bool
	Dev      bool
	UseTmpfs      bool     // overlay or empty tmpfs /
}

type FSPhase int

const (
	FSPhasePrePivot FSPhase = iota
	FSPhasePostPivot
)

type FSMount struct {
	Source string
	Target string
	FSType string
	Flags  uintptr
}

//func SnapshotMounts() ([]FSMount, error) 


/*
func ApplyFilesystem(cfg FSConfig) error

//tests
func PrepareRoot(cfg FSConfig) error  - done
func PivotRoot(newRoot string) error - done
func MountSpecials(cfg FSConfig) error
func RemountReadOnly(cfg FSConfig) error
func VerifyNoHostLeak() error
*/

func denySetgroups(pidStr string) error {
	return os.WriteFile("/proc/"+pidStr+"/setgroups", []byte("deny"), 0644)
}

func writeUIDMap(pidStr string) error {
	uid := os.Getuid()
	data := fmt.Sprintf("0 %d 1\n", uid)
	return os.WriteFile("/proc/"+pidStr+"/uid_map", []byte(data), 0644)
}

func writeGIDMap(pidStr string) error {
	gid := os.Getgid()
	data := fmt.Sprintf("0 %d 1\n", gid)
	return os.WriteFile("/proc/"+pidStr+"/gid_map", []byte(data), 0644)
}

func SetupUserNamespace(pidStr string) error {	
	
	if err := writeUIDMap(pidStr); err != nil {
		return fmt.Errorf("uid_map: %w", err)
	}
	
	if err := denySetgroups(pidStr); err != nil {
		return fmt.Errorf("setgroups: %w", err)
	}
	
	if err := writeGIDMap(pidStr); err != nil {
		return fmt.Errorf("gid_map: %w", err)
	}
	return nil
}




func PrepareRoot(cfg FSConfig) error {
	// 1. Make mounts private (critical)
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make / private: %w", err)
	}

	// 2. Validate rootfs
	st, err := os.Stat(cfg.Rootfs)
	if err != nil {
		return fmt.Errorf("rootfs stat: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("rootfs is not a directory")
	}	

	// 3. Bind-mount rootfs onto itself (required for pivot_root)
	if err := unix.Mount(
		cfg.Rootfs,
		cfg.Rootfs,
		"",
		unix.MS_BIND|unix.MS_REC,
		"",
	); err != nil {
		return fmt.Errorf("bind rootfs: %w", err)
	}

	// 4. Make rootfs mount private explicitly
	if err := unix.Mount(
		"",
		cfg.Rootfs,
		"",
		unix.MS_REC|unix.MS_PRIVATE,
		"",
	); err != nil {
		return fmt.Errorf("make rootfs private: %w", err)
	}

	// 5. Prepare /dev and bind host devices
	devPath := filepath.Join(cfg.Rootfs, "dev")
	if err := os.MkdirAll(devPath, 0755); err != nil {
		return err
	}

	for _, d := range []string{"null", "zero", "urandom"} {
		target := filepath.Join(devPath, d)

		// create empty file as mount target
		f, err := os.OpenFile(target, os.O_CREATE, 0666)
		if err != nil {
			return err
		}
		f.Close()

		if err := unix.Mount("/dev/"+d, target, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind /dev/%s: %w", d, err)
		}
	}

	// 6. Create pivot directories
	if err := os.MkdirAll(filepath.Join(cfg.Rootfs, ".pivot_old"), 0700); err != nil {
		return fmt.Errorf("mkdir oldroot: %w", err)
	}


	

	return nil
}

func PivotRoot(newRoot string) error {
	putOld := filepath.Join(newRoot, ".pivot_old")

	// 1. Change to new root (required)
	if err := unix.Chdir(newRoot); err != nil {
		return fmt.Errorf("chdir new root: %w", err)
	}

	// 2. pivot_root(newRoot, newRoot/.pivot_old)
	if err := unix.PivotRoot(newRoot, putOld); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	// 3. Now "/" is newRoot
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// 4. Unmount old root
	if err := unix.Unmount("/.pivot_old", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("umount old root: %w", err)
	}

	// 5. Remove old root directory
	if err := os.RemoveAll("/.pivot_old"); err != nil {
		return fmt.Errorf("remove old root: %w", err)
	}

	return nil
}

func MountVirtualFS(cfg FSConfig) error {

	if cfg.Proc {
		_ = unix.Unmount("/proc", unix.MNT_DETACH)

		if err := unix.Mount(
			"proc",
			"/proc",
			"proc",
			unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV,
			"",
		); err != nil && err != unix.EPERM {
			return fmt.Errorf("proc mount failed: %w", err)
		}
	}

	if cfg.Sys {
		if err := unix.Mount(
			"sysfs",
			"/sys",
			"sysfs",
			unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV,
			"",
		); err != nil && err != unix.EPERM {
			return fmt.Errorf("sysfs mount failed: %w", err)
		}
	}

	/*
	if cfg.Dev {
		if err := unix.Mount(
			"tmpfs",
			"/dev",
			"tmpfs",
			unix.MS_NOSUID|unix.MS_STRICTATIME|unix.MS_NOEXEC,
			"mode=755",
		); err != nil {
			return err
		}		
	}
		*/



	return nil
}




func TestFS() {
	fsCfg := FSConfig{
		Rootfs:   "/dev/shm/bctor-root",
		ReadOnly: false,
		Proc:     true,
		Sys:      true,
		Dev:      true,
		UseTmpfs: false,
	}

	pid, err := NewFork()
	if err != nil {
		fmt.Fprintln(os.Stderr, "NewFork failed:", err)
		return
	}

	if pid == 0 {
		// CHILD

		// 1. Prepare filesystem (mounts, bind, propagation)
		if err := PrepareRoot(fsCfg); err != nil {
			fmt.Fprintln(os.Stderr, "PrepareRoot failed:", err)
			unix.Exit(1)
		}

		// 2. pivot_root
		if err := PivotRoot(fsCfg.Rootfs); err != nil {
			fmt.Fprintln(os.Stderr, "PivotRoot failed:", err)
			unix.Exit(1)
		}

		// 3. Recreate mount points inside new root
		_ = os.MkdirAll("/proc", 0555)
		_ = os.MkdirAll("/sys", 0555)
		_ = os.MkdirAll("/dev", 0755)

		// 4. Mount virtual filesystems
		if err := MountVirtualFS(fsCfg); err != nil {
			fmt.Fprintln(os.Stderr, "MountVirtualFS failed:", err)
			unix.Exit(1)
		}

		// 5. Exec target
		/*
		if err := unix.Exec(
			"/bin/ls",
			[]string{"ls", "-la", "/"},
			[]string{"PATH=/bin"},
		); err != nil {
			fmt.Fprintln(os.Stderr, "Exec failed:", err)
			unix.Exit(127)
		}
			*/

		_ = unix.Exec("/bin/sh", []string{"sh"}, []string{"PATH=/bin"})


		// unreachable
		unix.Exit(0)

	} else {
		// PARENT
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	}
}
