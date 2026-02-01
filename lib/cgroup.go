package lib

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type CGroupsConfig struct {
    CPUMax    string // "100000 1000000" → quota & period
    MemoryMax string // "256M"
    PIDsMax   string // "10"
    IOMax     string // "8:0 rbps=1048576 wbps=1048576"
    Path      string // "/sys/fs/cgroup/bctor"
}

func ApplyCgroups(cfg CGroupsConfig) error {
    // Create the cgroup dir
    if err := os.MkdirAll(cfg.Path, 0755); err != nil {
        return fmt.Errorf("failed to create cgroup: %w", err)
    }

    // Write limits
    if cfg.CPUMax != "" {
        os.WriteFile(filepath.Join(cfg.Path, "cpu.max"), []byte(cfg.CPUMax), 0644)
    }
    if cfg.MemoryMax != "" {
        os.WriteFile(filepath.Join(cfg.Path, "memory.max"), []byte(cfg.MemoryMax), 0644)
    }
    if cfg.PIDsMax != "" {
        os.WriteFile(filepath.Join(cfg.Path, "pids.max"), []byte(cfg.PIDsMax), 0644)
    }
    if cfg.IOMax != "" {
        os.WriteFile(filepath.Join(cfg.Path, "io.max"), []byte(cfg.IOMax), 0644)
    }

    // Move current PID into the new cgroup
    pid := strconv.Itoa(os.Getpid())
    return os.WriteFile(filepath.Join(cfg.Path, "cgroup.procs"), []byte(pid), 0644)
}

func RemoveCgroups(cfg CGroupsConfig) error {
    return os.RemoveAll(cfg.Path)
}

type CgroupSnapshot map[string]string

func SnapshotCgroup(path string, files []string) CgroupSnapshot {
	snap := make(CgroupSnapshot)

	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(path, f))
		if err != nil {
			snap[f] = "<unreadable>"
			continue
		}
		snap[f] = strings.TrimSpace(string(data))
	}
	return snap
}

func DiffCgroup(before, after CgroupSnapshot) {
	for k, vBefore := range before {
		if vAfter, ok := after[k]; ok && vBefore != vAfter {
			fmt.Printf("[CG] %-15s : %q → %q\n", k, vBefore, vAfter)
		}
	}
}

func MountCgroup2() error {
	var st unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &st); err != nil {
		return fmt.Errorf("No such file or dir: /sys/fs/cgroup")
	}
	if st.Type != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("cgroup v2 not mounted")
	}
	return nil
}


func EnableControllers(root string, ctrls []string) error {
	data := "+" + strings.Join(ctrls, " +")
	return os.WriteFile(
		filepath.Join(root, "cgroup.subtree_control"),
		[]byte(data),
		0644,
	)
}


func TestCgroups() {	

	/*
		
		lib.SetCapabilities(lib.CAP_SYS_ADMIN)
	_ = lib.AddEffective(lib.CAP_SYS_ADMIN)
	_ = lib.AddInheritable(lib.CAP_SYS_ADMIN)
	_ = lib.AddPermitted(lib.CAP_SYS_ADMIN)
	_ = lib.RaiseAmbient(lib.CAP_SYS_ADMIN)
*/	

	files := []string{
		"cpu.max",
		"memory.max",
		"pids.max",
		"io.max",
		"cgroup.procs",
	}

	cfg := CGroupsConfig{
		Path:      "/sys/fs/cgroup/bctor",
		CPUMax:    "50000 100000", // 50% CPU
		MemoryMax: "128M",
		PIDsMax:   "5",
	}

	fmt.Println("[*] Init of TestCGroups")

	if err := MountCgroup2(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("[*] MountCgroup2")
	
	err := EnableControllers("/sys/fs/cgroup", []string{
		"cpu",
		"memory",
		"pids",
		"io",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("[*] EnableControllers")


	before := SnapshotCgroup("/sys/fs/cgroup", files)

	fmt.Println("[*] SnapshotCgroup")

	if err := ApplyCgroups(cfg); err != nil {
		log.Fatal(err)
	}

	fmt.Println("[*] ApplyCgroups")

	after := SnapshotCgroup(cfg.Path, files)
	DiffCgroup(before, after)

	

	// Now run the container / child process
	//unix.Exec("/proc/self/exe", []string{"bctor"}, os.Environ())

}