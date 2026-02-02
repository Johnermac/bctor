package lib

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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
        if err == nil {
            val := strings.TrimSpace(string(data))

            if val == "max" {
                switch f {
                case "memory.max":
                    snap[f] = getHostMemoryLimit()
                case "cpu.max":
                    snap[f] = getHostCPUQuota()
                case "pids.max":
                    snap[f] = getHostPIDLimit()
								case "io.max": 
										snap[f] = parseIOMax(path) 	
								case "cgroup.procs": 
										statData, err := os.ReadFile(filepath.Join(path, f)) 
										if err == nil { snap[f] = parseCgroupProcs(string(statData)) } else { snap[f] = "<unreadable>" }							
                default:
                    snap[f] = val
                }
            } else {
                snap[f] = val
            }
            continue
        }

        // Fallbacks if file unreadable
        switch f {
        case "memory.max":
            snap[f] = getHostMemoryLimit()
        case "cpu.max":
            snap[f] = getHostCPUQuota()
        case "pids.max":
            snap[f] = getHostPIDLimit()
				case "io.max": 
						snap[f] = parseIOMax(path)
				case "cgroup.procs": 
						statData, err := os.ReadFile(filepath.Join(path, f)) 
						if err == nil { snap[f] = parseCgroupProcs(string(statData)) } else { snap[f] = "<unreadable>" }
        default:
            snap[f] = "<unreadable>"
        }
    }
    return snap
}

// Helpers

func parseCgroupProcs(data string) string {
    lines := strings.Split(strings.TrimSpace(data), "\n")
    var pids []string
    for _, l := range lines {
        l = strings.TrimSpace(l)
        if l != "" && l != "0" {
            pids = append(pids, l)
        }
    }

    // return count
    //return fmt.Sprintf("%d", len(pids))

    //return pids
     return strings.Join(pids, ",")
}


func parseIOMax(path string) string {
    data, err := os.ReadFile(filepath.Join(path, "io.max"))
    if err == nil && strings.TrimSpace(string(data)) != "" {
        return strings.TrimSpace(string(data))
    }

    // fallback to io.stat
    statData, err := os.ReadFile(filepath.Join(path, "io.stat"))
    if err != nil {
        return "<unreadable>"
    }

    var rbytes, wbytes uint64
    for _, line := range strings.Split(strings.TrimSpace(string(statData)), "\n") {
        fields := strings.Fields(line)
        for _, f := range fields {
            if strings.HasPrefix(f, "rbytes=") {
                val, _ := strconv.ParseUint(strings.TrimPrefix(f, "rbytes="), 10, 64)
                rbytes += val
            }
            if strings.HasPrefix(f, "wbytes=") {
                val, _ := strconv.ParseUint(strings.TrimPrefix(f, "wbytes="), 10, 64)
                wbytes += val
            }
        }
    }
    return fmt.Sprintf("rbytes=%d wbytes=%d", rbytes, wbytes)
}


func getHostMemoryLimit() string {
    data, err := os.ReadFile("/proc/meminfo")
    if err != nil {
        return "<unreadable>"
    }
    for _, line := range strings.Split(string(data), "\n") {
        if strings.HasPrefix(line, "MemTotal:") {
            fields := strings.Fields(line)
            if len(fields) >= 2 {
                // MemTotal is in kB
                kb, _ := strconv.ParseUint(fields[1], 10, 64)
                return fmt.Sprintf("%d", kb*1024) // bytes
            }
        }
    }
    return "<unreadable>"
}

func getHostCPUQuota() string {
    //  fallback
    n := runtime.NumCPU()
    return fmt.Sprintf("%d CPUs", n)
}

func getHostPIDLimit() string {
    // Fallback
    data, err := os.ReadFile("/proc/sys/kernel/pid_max")
    if err != nil {
        return "<unreadable>"
    }
    return strings.TrimSpace(string(data))
}


func DiffCgroup(before, after CgroupSnapshot) {
	for k, vBefore := range before {
		if vAfter, ok := after[k]; ok && vBefore != vAfter {
			fmt.Printf("[CG] %-15s : %q → %q\n", k, vBefore, vAfter)
		}
	}
}

func CheckCgroupV2() error {
	var st unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &st); err != nil {
		return fmt.Errorf("No such file or dir: /sys/fs/cgroup")
	}
	if st.Type != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("cgroup v2 not mounted")
	}
	return nil
}

func SetCgroupFreeze(cgPath string, freeze bool) error {
	val := "0"
	if freeze {
		val = "1"
	}
	return os.WriteFile(filepath.Join(cgPath, "cgroup.freeze"), []byte(val), 0644)
}


func EnableControllers(root string, ctrls []string) error {
	data := "+" + strings.Join(ctrls, " +")
	return os.WriteFile(
		filepath.Join(root, "cgroup.subtree_control"),
		[]byte(data),
		0644,
	)
}


func TestCgroups(cfgNS NamespaceConfig) {		

	files := []string{
		"cpu.max",
		"memory.max",
		"pids.max",
		"io.max",
		"cgroup.procs",
	}

	cfg := CGroupsConfig{
		Path:      "/sys/fs/cgroup/bctor",
		CPUMax:    "10000 100000", // 10% CPU
		MemoryMax: "4M",
		PIDsMax:   "3",
	}

	fmt.Println("[*] Init of TestCGroups")

	if err := CheckCgroupV2(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("[*] CheckCgroupV2")
	
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

	if cfgNS.CGROUP {
		err := unix.Unshare(unix.CLONE_NEWCGROUP)
			if err != nil {
					os.Stdout.WriteString("Erro no Unshare CGROUP: " + err.Error() + "\n")
			}
	}

	fmt.Println("[*] ApplyCgroups")

	after := SnapshotCgroup(cfg.Path, files)
	DiffCgroup(before, after)

}