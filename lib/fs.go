package lib

import (
	"fmt"
	"os"
	"path/filepath"

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

func PrepareRoot(cfg FSConfig) error {
	// 1. Make mounts private (critical)
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("---[?] Workload: make / private: %w", err)
	}

	// 2. Bind-mount rootfs onto itself (required for pivot_root)

	if err := os.MkdirAll(cfg.Rootfs, 0755); err != nil {
		return fmt.Errorf("---[?] Workload: Failed to create rootfs dir: %w", err)
	}

	_, err := os.Stat(cfg.Rootfs)
	if err != nil {
		return fmt.Errorf("---[?] Workload: rootfs stat: %w", err)
	}

	if err := unix.Mount(
		cfg.Rootfs,
		cfg.Rootfs,
		"",
		unix.MS_BIND|unix.MS_REC,
		"",
	); err != nil {
		return fmt.Errorf("---[?] Workload: bind rootfs: %w", err)
	}

	// 3. Make rootfs mount private explicitly
	if err := unix.Mount(
		"",
		cfg.Rootfs,
		"",
		unix.MS_REC|unix.MS_PRIVATE,
		"",
	); err != nil {
		return fmt.Errorf("---[?] Workload: make rootfs private: %w", err)
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

	for _, cmd := range []string{"sh", "ls", "nc", "ip", "ping"} {
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
			return fmt.Errorf("---[?] Workload: bind /dev/%s: %w", d, err)
		}
	}

	// 6. Create pivot directories
	if err := os.MkdirAll(filepath.Join(cfg.Rootfs, ".pivot_old"), 0700); err != nil {
		return fmt.Errorf("---[?] Workload: Failed to mkdir oldroot: %w", err)
	}

	return nil
}

func PivotRoot(newRoot string) error {
	putOld := filepath.Join(newRoot, ".pivot_old")

	// 1. Change to new root (required)
	if err := unix.Chdir(newRoot); err != nil {
		return fmt.Errorf("---[?] Workload: chdir new root: %w", err)
	}

	// 2. pivot_root(newRoot, newRoot/.pivot_old)
	if err := unix.PivotRoot(newRoot, putOld); err != nil {
		return fmt.Errorf("---[?] Workload: pivot_root: %w", err)
	}

	// 3. Now "/" is newRoot
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("---[?] Workload: chdir /: %w", err)
	}

	// 4. Unmount old root
	if err := unix.Unmount("/.pivot_old", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("---[?] Workload: umount old root: %w", err)
	}

	// 5. Remove old root directory
	if err := os.RemoveAll("/.pivot_old"); err != nil {
		return fmt.Errorf("---[?] Workload: remove old root: %w", err)
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
			return fmt.Errorf("---[?] Workload: proc mount failed: %w", err)
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
			return fmt.Errorf("---[?] Workload: sysfs mount failed: %w", err)
		}
	}

	if cfg.UseTmpfs {
		writable := []string{"/tmp", "/run", "/var"}

		for _, dir := range writable {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("---[?] Workload: mkdir %s failed: %w", dir, err)
			}

			if err := unix.Mount(
				"tmpfs",
				dir,
				"tmpfs",
				unix.MS_NOSUID|unix.MS_NODEV,
				"size=64M",
			); err != nil {
				return fmt.Errorf("---[?] Workload: mount tmpfs on %s failed: %w", dir, err)
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
			return fmt.Errorf("---[?] Workload: remount rootfs readonly failed: %w", err)
		}
	}

	return nil
}

func FileSystemSetup(fsCfg FSConfig) {
	// 1. Prepare filesystem (mounts, bind, propagation)
	if err := PrepareRoot(fsCfg); err != nil {
		fmt.Fprintln(os.Stderr, "---[?] Workload: PrepareRoot failed:", err)
		unix.Exit(1)
	}

	// 2. pivot_root
	if err := PivotRoot(fsCfg.Rootfs); err != nil {
		fmt.Fprintln(os.Stderr, "---[?] Workload: PivotRoot failed:", err)
		unix.Exit(1)
	}

	// 3. Recreate mount points inside new root
	_ = os.MkdirAll("/proc", 0555)
	_ = os.MkdirAll("/sys", 0555)
	_ = os.MkdirAll("/dev", 0755)

	// 4. Mount virtual filesystems
	if err := MountVirtualFS(fsCfg); err != nil {
		fmt.Fprintln(os.Stderr, "---[?] Workload: MountVirtualFS failed:", err)
		unix.Exit(1)
	}

	fmt.Println("---[*] Workload: Calling HideProc")
	callHideProc()
}
