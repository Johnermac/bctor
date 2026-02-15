package lib

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func HideProcPaths(paths []string) error {
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			// Path might not exist in older kernels — ignore
			continue
		}

		if st.IsDir() {
			if err := hideDir(p); err != nil {
				return LogError("hide dir %s: %v", p, err)
			}
		} else {
			if err := hideFile(p); err != nil {
				return LogError("hide file %s: %v", p, err)
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

func DebugMountContext() {
	fmt.Println("========== MOUNT DEBUG ==========")

	// Identity
	fmt.Println("UID:", os.Getuid())
	fmt.Println("EUID:", os.Geteuid())
	fmt.Println("GID:", os.Getgid())
	fmt.Println("EGID:", os.Getegid())

	// Namespace identity
	if ns, err := os.Readlink("/proc/self/ns/mnt"); err == nil {
		fmt.Println("Mount NS:", ns)
	}
	if ns, err := os.Readlink("/proc/self/ns/user"); err == nil {
		fmt.Println("User NS:", ns)
	}
	if ns, err := os.Readlink("/proc/self/ns/pid"); err == nil {
		fmt.Println("PID NS:", ns)
	}

	// UID/GID maps (important if using CLONE_NEWUSER)
	if data, err := os.ReadFile("/proc/self/uid_map"); err == nil {
		fmt.Println("uid_map:\n", string(data))
	}
	if data, err := os.ReadFile("/proc/self/gid_map"); err == nil {
		fmt.Println("gid_map:\n", string(data))
	}
	if data, err := os.ReadFile("/proc/self/setgroups"); err == nil {
		fmt.Println("setgroups:\n", string(data))
	}

	// Capabilities
	if data, err := os.ReadFile("/proc/self/status"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, l := range lines {
			if strings.HasPrefix(l, "CapEff") ||
				strings.HasPrefix(l, "CapPrm") ||
				strings.HasPrefix(l, "CapBnd") {
				fmt.Println(l)
			}
		}
	}

	// Check /proc mount target
	if fi, err := os.Stat("/proc"); err == nil {
		fmt.Println("/proc exists, mode:", fi.Mode())
	} else {
		fmt.Println("/proc stat error:", err)
	}

	// Check if root is readonly
	if data, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, " / ") {
				fmt.Println("Root mountinfo:", line)
			}
		}
	}

	fmt.Println("=================================")
}

func PrepareRoot(cfg FSConfig) error {
	// 1. Make mounts private (critical)
	unix.Mount("", "/", "", unix.MS_REC|unix.MS_SLAVE, "")
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return LogError("Workload: make / private: %v", err)
	}

	// 2. Bind-mount rootfs onto itself (required for pivot_root)

	if err := os.MkdirAll(cfg.Rootfs, 0755); err != nil {
		return LogError("Workload: Failed to create rootfs dir: %v", err)
	}

	_, err := os.Stat(cfg.Rootfs)
	if err != nil {
		return LogError("Workload: rootfs stat: %v", err)
	}

	//os.RemoveAll(filepath.Join(cfg.Rootfs, "proc"))
	//os.MkdirAll(filepath.Join(cfg.Rootfs, "proc"), 0755)

	if err := unix.Mount(
		cfg.Rootfs,
		cfg.Rootfs,
		"",
		unix.MS_BIND|unix.MS_REC,
		"",
	); err != nil {
		return LogError("Workload: bind rootfs: %v", err)
	}

	// 3. Make rootfs mount private explicitly
	if err := unix.Mount(
		"",
		cfg.Rootfs,
		"",
		unix.MS_REC|unix.MS_PRIVATE,
		"",
	); err != nil {
		return LogError("Workload: make rootfs private: %v", err)
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
			return LogError("Workload: bind /dev/%s: %v", d, err)
		}
	}

	// 6. Create pivot directories
	if err := os.MkdirAll(filepath.Join(cfg.Rootfs, ".pivot_old"), 0700); err != nil {
		return LogError("Workload: Failed to mkdir oldroot: %v", err)
	}

	return nil
}

func PivotRoot(newRoot string) error {
	putOld := filepath.Join(newRoot, ".pivot_old")

	// 1. Change to new root (required)
	if err := unix.Chdir(newRoot); err != nil {
		return LogError("Workload: chdir new root: %v", err)
	}

	// 2. pivot_root(newRoot, newRoot/.pivot_old)
	if err := unix.PivotRoot(newRoot, putOld); err != nil {
		return LogError("Workload: pivot_root: %v", err)
	}

	// 3. Now "/" is newRoot
	if err := unix.Chdir("/"); err != nil {
		return LogError("Workload: chdir /: %v", err)
	}

	// 4. Unmount old root
	if err := unix.Unmount("/.pivot_old", unix.MNT_DETACH); err != nil {
		return LogError("Workload: umount old root: %v", err)
	}

	// 5. Remove old root directory
	if err := os.RemoveAll("/.pivot_old"); err != nil {
		return LogError("Workload: remove old root: %v", err)
	}

	return nil
}

func MountVirtualFS(cfg FSConfig) error {

	// Inside MountVirtualFS
	if cfg.Proc {
		// Try to remount to refresh the network view for this specific container
		flags := unix.MS_REMOUNT | unix.MS_NOSUID | unix.MS_NOEXEC | unix.MS_NODEV
		err := unix.Mount("proc", "/proc", "proc", uintptr(flags), "")
		if err != nil {
			// If remount fails, do the hard unmount/mount
			_ = unix.Unmount("/proc", unix.MNT_DETACH)
			_ = unix.Mount("proc", "/proc", "proc", 0, "")
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
			return LogError("Workload: sysfs mount failed: %v", err)
		}
	}

	if cfg.UseTmpfs {
		writable := []string{"/tmp", "/run", "/var"}

		for _, dir := range writable {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return LogError("Workload: mkdir %s failed: %v", dir, err)
			}

			if err := unix.Mount(
				"tmpfs",
				dir,
				"tmpfs",
				unix.MS_NOSUID|unix.MS_NODEV,
				"size=64M",
			); err != nil {
				return LogError("Workload: mount tmpfs on %s failed: %v", dir, err)
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
			return LogError("Workload: remount rootfs readonly failed: %v", err)
		}
	}

	return nil
}
