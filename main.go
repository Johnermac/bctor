package main

import (
	"os"
	"runtime"
	"strconv"

	"github.com/Johnermac/bctor/lib"
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
		lib.ReadNamespaces(pidStr)

		selfExe, _ := os.Readlink("/proc/self/exe")

		for range 50 {

			childExe, err := os.Readlink("/proc/" + pidStr + "/exe")
			//readExe(childExe)
			if err == nil && childExe != selfExe {
				lib.ReadIdentity(pidStr)
				lib.ReadCapabilities(pidStr)
				lib.ReadCgroups(pidStr)
				lib.ReadSyscalls(pidStr)
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


