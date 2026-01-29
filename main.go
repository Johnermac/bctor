package main

import (
	"os"
	"runtime"

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

	// -------------------------------- PRINT PARENT

	//os.Stdout.WriteString("=== NAMESPACES PARENT (Host) ===\n")
	//pp := strconv.Itoa(os.Getpid())
	parentNS, _ := lib.ReadNamespaces(os.Getpid())
	// optional debug
	//lib.LogNamespacePosture("parent", parentNS)

	capStateBefore, err := lib.ReadCaps(os.Getpid())
	if err != nil {
		os.Stdout.WriteString("Error in ReadCaps for PARENT: " + err.Error() + "\n")
	}
	//lib.LogCaps("PARENT", capStateBefore)
	//lib.LogCapPosture("parent (initial)", capStateBefore)

	pid, _, errno := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
	if errno != 0 {
		// unrecoverable
		unix.Exit(1)
	}

	if pid == 0 {
		// ----------------
		// Child path
		// ----------------

		unix.Close(fd[0])

		cfg := lib.NamespaceConfig{
			USER: true,
			//MOUNT: true,
			//PID: true,
			//UTS: true,
			//NET: true,
			//IPC: true,
		}
		

		err := lib.ApplyNamespaces(cfg)
		if err != nil {
			os.Stdout.WriteString("Error while applying NS: " + err.Error() + "\n")
			unix.Exit(1)
		}

		// -------------------------------- PRINT CHILD
		//os.Stdout.WriteString("\n=== NAMESPACES CHILD (Container) ===\n")
		//gp := strconv.Itoa(os.Getpid())
		childNS, _ := lib.ReadNamespaces(os.Getpid())

		nsdiff := lib.DiffNamespaces(parentNS, childNS)
		lib.LogNamespaceDelta(nsdiff)

		//optional debug
		//lib.LogNamespacePosture("child", childNS)		

		capStateAfter, err := lib.ReadCaps(os.Getpid())
		if err != nil {
			os.Stdout.WriteString("Error in ReadCaps for CHILD: " + err.Error() + "\n")
		}

		diff := lib.DiffCaps(capStateBefore, capStateAfter)
		if len(diff) > 0 {
			lib.LogCapDelta(diff)
		}
		// optional for debug
		//lib.LogCaps("CHILD", capStateAfter)	
		//lib.LogCapPosture("child (post-namespaces)", capStateAfter)		

		role, grandchildHostPid, err := lib.ResolvePIDNamespace(cfg.PID)
		if err != nil {
			os.Stdout.WriteString("Error in ResolvePIDNamespace: " + err.Error() + "\n")
			unix.Exit(1)
		}

		switch role {
		case lib.PIDRoleExit:
			// --------------------HERE CHILD IS PRINTING GRAND-CHILD INFO
			grandchildNS, _ := lib.ReadNamespaces(grandchildHostPid)
			nsdiff := lib.DiffNamespaces(parentNS, grandchildNS)
			lib.LogNamespaceDelta(nsdiff)
			// optional
			// lib.LogNamespacePosture("grand-child", grandchildNS)	
			unix.Exit(0)
		case lib.PIDRoleInit, lib.PIDRoleContinue:						
			path := "/bin/true"
			err = unix.Exec(path, []string{path}, os.Environ())
			if err != nil {
				os.Stdout.WriteString("Error in unix.Exec PIDRoleContinue: " + err.Error() + "\n")
			}
		}
	} else {
		// ----------------
		// Parent path
		// ----------------
		unix.Close(fd[1]) // close write end
		//pidStr := strconv.Itoa(int(pid)) child pid		
		
		// wait for EOF on pipe
		buf := make([]byte, 1)
		_, _ = unix.Read(fd[0], buf)
		unix.Close(fd[0])

		// reap child
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	}
}
