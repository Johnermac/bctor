package lib

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type FSConfig struct {
	Rootfs   string // path to prepared rootfs
	ReadOnly bool
	Proc     bool
	Sys      bool
	Dev      bool
	UseTmpfs bool // overlay or empty tmpfs /
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

func HideProcPaths(paths []string) error {
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			// Path might not exist in older kernels — ignore
			continue
		}

		if st.IsDir() {
			if err := hideDir(p); err != nil {
				return fmt.Errorf("hide dir %s: %w", p, err)
			}
		} else {
			if err := hideFile(p); err != nil {
				return fmt.Errorf("hide file %s: %w", p, err)
			}
		}
	}
	return nil
}

func hideFile(path string) error {
	if err := unix.Mount("/dev/null", path, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	return nil
}

func hideDir(path string) error {
	if err := unix.Mount("tmpfs", path, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=0"); err != nil {
		return err
	}
	return nil
}

func callHideProc() {
	hide := []string{
		"/proc/kcore",
		"/proc/keys",
		"/proc/key-users",
		"/proc/sysrq-trigger",
		"/proc/latency_stats",
		"/proc/timer_list",
		"/proc/sched_debug",
		"/proc/interrupts",
		"/proc/slabinfo",
		"/proc/meminfo",
		"/proc/iomem",
		"/proc/kallsyms",
		"/proc/modules",
	}

	if err := HideProcPaths(hide); err != nil {
		return
	}
}

// little helper
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}

func PrepareRoot(cfg FSConfig) error {
	// 1. Make mounts private (critical)
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make / private: %w", err)
	}

	// 2. Bind-mount rootfs onto itself (required for pivot_root)

	if err := os.MkdirAll(cfg.Rootfs, 0755); err != nil {
		return fmt.Errorf("failed to create rootfs dir: %w", err)
	}

	_, err := os.Stat(cfg.Rootfs)
	if err != nil {
		return fmt.Errorf("rootfs stat: %w", err)
	}

	if err := unix.Mount(
		cfg.Rootfs,
		cfg.Rootfs,
		"",
		unix.MS_BIND|unix.MS_REC,
		"",
	); err != nil {
		return fmt.Errorf("bind rootfs: %w", err)
	}

	// 3. Make rootfs mount private explicitly
	if err := unix.Mount(
		"",
		cfg.Rootfs,
		"",
		unix.MS_REC|unix.MS_PRIVATE,
		"",
	); err != nil {
		return fmt.Errorf("make rootfs private: %w", err)
	}

	// 4. setup busybox shell	

	binDir := filepath.Join(cfg.Rootfs, "bin")
	os.MkdirAll(binDir, 0755)

	busyboxDest := filepath.Join(binDir, "busybox")
	if _, err := os.Stat(busyboxDest); os.IsNotExist(err) {
		input, _ := os.ReadFile("/bin/busybox")
		os.WriteFile(busyboxDest, input, 0755)

		f, _ := os.Open(busyboxDest)
		f.Sync()
		f.Close()
	}

	for _, cmd := range []string{"sh", "ls", "nc"} {
		target := filepath.Join(binDir, cmd)
		os.Remove(target)
		os.Symlink("busybox", target)
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

	if cfg.UseTmpfs {
		writable := []string{"/tmp", "/run", "/var"}

		for _, dir := range writable {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("mkdir %s failed: %w", dir, err)
			}

			if err := unix.Mount(
				"tmpfs",
				dir,
				"tmpfs",
				unix.MS_NOSUID|unix.MS_NODEV,
				"size=64M",
			); err != nil {
				return fmt.Errorf("mount tmpfs on %s failed: %w", dir, err)
			}
		}
	}

	if cfg.ReadOnly {
		if err := unix.Mount(
			"",
			"/",
			"",
			unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_REC,
			"",
		); err != nil {
			return fmt.Errorf("remount rootfs readonly failed: %w", err)
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

func TestFS(path string) {
	fsCfg := FSConfig{
		Rootfs:   path,
		ReadOnly: false, // no permission, debug later
		Proc:     true,
		Sys:      true,
		Dev:      true,
		UseTmpfs: true,
	}

	pid, err := NewFork()
	if err != nil {
		fmt.Fprintln(os.Stderr, "NewFork failed:", err)
		return
	}

	if pid == 0 {
		// GRAND-CHILD

		/*capStateChild, _ := ReadCaps(os.Getpid())
		if err != nil {
			os.Stdout.WriteString("Error in ReadCaps: " + err.Error() + "\n")
		}
		LogCapPosture("grand-child", capStateChild)
		*/

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

		fmt.Println("[*] callHideProc")
		callHideProc()

		fmt.Println("[*] Apply Seccomp")

		profile := ProfileWorkload //set to arg in the future

		err = ApplySeccomp(profile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ApplySeccomp failed:", err)
		}
		fmt.Println("[*] Profile loaded")

		if profile == ProfileDebugShell {
			_ = unix.Exec("/bin/sh", []string{"sh"}, []string{"PATH=/bin"})
		}

		if profile == ProfileHello {
			message := "\n[!] Hello Seccomp!\n"
			syscall.Write(1, []byte(message))
			syscall.Exit(0)
		}

		if profile == ProfileWorkload {
			err = unix.Exec("/bin/nc", []string{"nc","-lp","4445"}, os.Environ())
			if err != nil {
				fmt.Fprintln(os.Stderr, "nc failed:", err)
			}
		}

		

		// unreachable
		unix.Exit(0)

	} else {
		// PARENT
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	}
}
