package lib

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func StatusCalls(pidStr string) {
	readNamespaces(pidStr)
	//readIdentity(pidStr)
	//readCapabilities(pidStr)
	//readCgroups(pidStr)
	//readSyscalls(pidStr)
}

func ReadExe(exePath string) {
	// Print exec path
	os.Stdout.WriteString("EXE=" + exePath + "\n")
	//return "EXE=" + exePath + "\n"

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

func readNamespaces(pidStr string) {
	nsPaths := []string{"mnt", "pid", "net", "uts", "ipc", "user"}

	for _, ns := range nsPaths {
		target, err := os.Readlink("/proc/" + pidStr + "/ns/" + ns)
		if err != nil {
			os.Stdout.WriteString("NS_" + ns + "=error\n")
			continue
		}
		// target looks like "mnt:[4026531840]"
		parts := strings.Split(target, ":")
		if len(parts) == 2 {
			id := strings.Trim(parts[1], "[]")
			os.Stdout.WriteString("NS_" + ns + "=" + id + "\n")
		}
	}
}
