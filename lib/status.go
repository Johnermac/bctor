package lib

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type Namespace string

const (
	NS_MNT  Namespace = "mnt"
	NS_PID  Namespace = "pid"
	NS_NET  Namespace = "net"
	NS_UTS  Namespace = "uts"
	NS_IPC  Namespace = "ipc"
	NS_USER Namespace = "user"
)

type NamespaceState struct {
	PID int
	IDs map[Namespace]string
}

type NamespaceDiff struct {
	Namespace Namespace
	Before    string
	After     string
}

func StatusCalls(pid int) (*NamespaceState, error) {
	return ReadNamespaces(pid)
	//readIdentity(pidStr)
	//readCapabilities(pidStr)
	//readCgroups(pidStr)
	//readSyscalls(pidStr)
}

func readIdentity(pidStr string) {
	statusPath := ("/proc/" + pidStr + "/status")
	data, err := os.ReadFile(statusPath)
	if err != nil {
		unix.Exit(1)
	}

	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "Pid:":
			if len(fields) >= 2 {
				os.Stdout.WriteString("PID=" + fields[1] + "\n")
			}
		case "PPid:":
			if len(fields) >= 2 {
				os.Stdout.WriteString("PPID=" + fields[1] + "\n")
			}
		case "Uid:":
			if len(fields) >= 3 {
				os.Stdout.WriteString("UID=" + fields[1] + " EUID=" + fields[2] + "\n")
			}
		case "Gid:":
			if len(fields) >= 3 {
				os.Stdout.WriteString("GID=" + fields[1] + " EGID=" + fields[2] + "\n")
			}
		}
	}
}

func readCapabilities(pidStr string) {
	statusPath := ("/proc/" + pidStr + "/status")
	data, err := os.ReadFile(statusPath)
	if err != nil {
		unix.Exit(1)
	}

	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "CapInh:": // Inheritable
			if len(fields) >= 2 {
				os.Stdout.WriteString("CapInh=" + fields[1] + "\n")
			}
		case "CapPrm:": // Permitted
			if len(fields) >= 2 {
				os.Stdout.WriteString("CapPrm=" + fields[1] + "\n")
			}
		case "CapEff:": //Effective
			if len(fields) >= 2 {
				os.Stdout.WriteString("CapEff=" + fields[1] + "\n")
			}
		case "CapBnd:": //Bounding
			if len(fields) >= 2 {
				os.Stdout.WriteString("CapBnd=" + fields[1] + "\n")
			}
		case "CapAmb:": //Ambient
			if len(fields) >= 2 {
				os.Stdout.WriteString("CapAmb=" + fields[1] + "\n")
			}
		}
	}
}

func readCgroups(pidStr string) {
	cgroupPath := ("/proc/" + pidStr + "/cgroup")
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		unix.Exit(1)
	}

	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		// log verbatim
		os.Stdout.WriteString(line + "\n")

		// split into hierarchy ID, controllers, cgroup path
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 {
			hierarchyID := parts[0]
			controllers := parts[1]
			cgroupPath := parts[2]

			os.Stdout.WriteString("HierarchyID=" + hierarchyID + "\n")
			os.Stdout.WriteString("Controllers=" + controllers + "\n")
			os.Stdout.WriteString("CgroupPath=" + cgroupPath + "\n")
		}
	}
}

func readSyscalls(pidStr string) {
	syscallPath := ("/proc/" + pidStr + "/syscall")
	data, err := os.ReadFile(syscallPath)
	if err != nil {
		// process may have exited or exec'd between checks
		return
	}

	// raw snapshot, do not parse
	os.Stdout.WriteString("SYSCALL=" + strings.TrimSpace(string(data)) + "\n")
}

func ReadNamespaces(pid int) (*NamespaceState, error) {
	nsList := []Namespace{NS_MNT, NS_PID, NS_NET, NS_UTS, NS_IPC, NS_USER}

	state := &NamespaceState{
		PID: pid,
		IDs: make(map[Namespace]string),
	}

	for _, ns := range nsList {
		link := fmt.Sprintf("/proc/%d/ns/%s", pid, ns)
		target, err := os.Readlink(link)
		if err != nil {
			state.IDs[ns] = "error"
			continue
		}

		// Example: "mnt:[4026531840]"
		if i := strings.Index(target, "["); i != -1 {
			state.IDs[ns] = strings.TrimSuffix(target[i+1:], "]")
		} else {
			state.IDs[ns] = target
		}
	}

	return state, nil
}

func DiffNamespaces(before, after *NamespaceState) []NamespaceDiff {
	var diffs []NamespaceDiff

	for ns, beforeID := range before.IDs {
		afterID := after.IDs[ns]
		if beforeID != afterID {
			diffs = append(diffs, NamespaceDiff{
				Namespace: ns,
				Before:    beforeID,
				After:     afterID,
			})
		}
	}

	return diffs
}

func LogNamespacePosture(label string, ns *NamespaceState) {
	fmt.Printf("\n[NS] %s (pid=%d)\n", label, ns.PID)

	order := []Namespace{NS_USER, NS_MNT, NS_PID, NS_NET, NS_IPC, NS_UTS}
	for _, n := range order {
		fmt.Printf("  %-4s → %s\n", n, ns.IDs[n])
	}
}

func LogNamespaceDelta(diffs []NamespaceDiff) {
	if len(diffs) == 0 {
		fmt.Println("\n[NS] no namespace transitions detected")
		return
	}
	
	for _, d := range diffs {
		fmt.Printf("  %-4s: %s → %s\n", d.Namespace, d.Before, d.After)
	}
}
