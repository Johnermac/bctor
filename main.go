package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func main() {
	// Critical: prevent Go runtime thread migration before fork
	runtime.LockOSThread()

	// fd[0] is the read end, fd[1] is the write end
	var fd [2]int
	err := unix.Pipe2(fd[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	pid, _, errno := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
	if errno != 0 {
		// unrecoverable
		unix.Exit(1)
	}

	if pid == 0 {
		// ----------------
		// Child path
		// ----------------

		// Close read end, then exec
		unix.Close(fd[0])
		path := "/bin/true"
		err := unix.Exec(path, []string{path}, []string{})
		if err != nil {
			unix.Exit(127)
		}

	} else {
		// ----------------
		// Parent path
		// ----------------
		// Close write end immediately
		unix.Close(fd[1])
		pidStr := strconv.Itoa(int(pid))
		readNamespaces(pidStr)

		selfExe, _ := os.Readlink("/proc/self/exe")

		for range 50 {

			childExe, err := os.Readlink("/proc/" + pidStr + "/exe")
			//readExe(childExe)
			if err == nil && childExe != selfExe {
				readIdentity("/proc/" + pidStr + "/status")
				readCapabilities("/proc/" + pidStr + "/status")
				readCgroups("/proc/" + pidStr + "/cgroup")
				break
			}
		}

		// Wait for EOF on the pipe (signifies Exec happened)
		buf := make([]byte, 1)
		n, _ := unix.Read(fd[0], buf)
		unix.Close(fd[0])

		if n == 0 {
			os.Stdout.WriteString("EXEC_CONFIRMED=true\n")
		} else {
			os.Stdout.WriteString("EXEC_CONFIRMED=false\n")
		}

		// For now, just wait to avoid zombie
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	}
}

func readExe(exePath string) {
	// Print exec path
	os.Stdout.WriteString("EXE=" + exePath + "\n")
	//return "EXE=" + exePath + "\n"

}

func readIdentity(statusPath string) {
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

func readCapabilities(statusPath string) {
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

func readCgroups(cgroupPath string) {
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
