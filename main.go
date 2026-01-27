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
	
	pid, _, errno := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
	if errno != 0 {
		// unrecoverable
		unix.Exit(1)
	}

	if pid == 0 {
		// ----------------
		// Child path
		// ----------------

		// Hardcode for now
		path := "/bin/true"

		argv := []string{path}
		envp := []string{}

		// Execve 
		err := unix.Exec(path, argv, envp)
		if err != nil {
			// Exec failed 
			unix.Exit(127)
		}

		// unreachable
	} else {
		// ----------------
		// Parent path
		// ----------------

		_ = pid // child PID

		pidStr := strconv.Itoa(int(pid))
		statusPath := "/proc/" + pidStr + "/status"

		data, err := os.ReadFile(statusPath)
		if err != nil {			
			unix.Exit(1)
		}

		lines := strings.Split(string(data), "\n")		

		for _, line := range lines {
			fields := strings.Fields(line)    
			
			if len(fields) < 2 {
					continue
			}

			switch fields[0] {
				case "Pid:":
						os.Stdout.WriteString("PID=" + fields[1] + "\n")
				case "PPid:":
						os.Stdout.WriteString("PPID=" + fields[1] + "\n")	
				case "Uid:":
						// fields[1] is Real UID, fields[2] is Effective UID
						os.Stdout.WriteString("UID=" + fields[1] + " EUID="+fields[2]+"\n")
				case "Gid:":
						os.Stdout.WriteString("GID=" + fields[1] + " EGID="+fields[2]+"\n")	
				}				
		}

		
			

		// For now, just wait to avoid zombie
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	}
}
