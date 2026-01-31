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
	defer runtime.UnlockOSThread()

	// fd[0] is the read end, fd[1] is the write end
	var p2c [2]int
	err := unix.Pipe2(p2c[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	var c2p [2]int
	err = unix.Pipe2(c2p[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	// -------------------------------- PRINT PARENT

	parentNS, _ := lib.ReadNamespaces(os.Getpid())
	// optional debug
	//lib.LogNamespacePosture("parent", parentNS)

	//capStateBefore, _ := lib.ReadCaps(os.Getpid())
	//if err != nil {
	//os.Stdout.WriteString("Error in ReadCaps for PARENT: " + err.Error() + "\n")
	//}
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

		unix.Close(p2c[1]) 
    unix.Close(c2p[0])

		cfg := lib.NamespaceConfig{
			USER: true,
			MOUNT: true,
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
		childNS, _ := lib.ReadNamespaces(os.Getpid())

		nsdiff := lib.DiffNamespaces(parentNS, childNS)
		lib.LogNamespaceDelta(nsdiff)

		//optional debug
		//lib.LogNamespacePosture("child", childNS)	


		// parent waiting...		 

    os.Stdout.WriteString("\n1     - pipe handshake started with parent\n")
    unix.Write(c2p[1], []byte("G")) 
    
    buf := make([]byte, 1)
    unix.Read(p2c[0], buf) 
    
    os.Stdout.WriteString("3     - finished like chads\n") 	
		
		/*
		lib.SetCapabilities(lib.CAP_SYS_ADMIN)
		_ = lib.AddEffective(lib.CAP_SYS_ADMIN)
		_ = lib.AddInheritable(lib.CAP_SYS_ADMIN)
		_ = lib.AddPermitted(lib.CAP_SYS_ADMIN)
		*/

		
		if cfg.MOUNT {			
			lib.TestFS()
		}

		// -------------------------------- CAPABILITY PART
		//lib.TestCap()

		// optional for debug
		//lib.LogCaps("CHILD", capStateAfter)
		//lib.LogCapPosture("child (post-namespaces)", capStateAfter)
		if cfg.PID {
			lib.TestPIDNS(parentNS, cfg)
		}
		
	} else {
		// ----------------
		// Parent path
		// ----------------
		
		unix.Close(p2c[0]) // Pai só escreve no p2c
    unix.Close(c2p[1]) // Pai só lê do c2p

    // 1. Espera o Filho avisar que nasceu
    buf := make([]byte, 1)
    unix.Read(c2p[0], buf)

    os.Stdout.WriteString("2     - ok buddy\n")

		pidStr := strconv.Itoa(int(pid)) //child pid

		
		if err := lib.SetupUserNamespace(pidStr); err != nil {
			os.Stdout.WriteString("SetupUserNamespace failed: " + err.Error() + "\n")
			unix.Exit(1)
		}

		os.Stdout.WriteString("2-yey - parent set up user namespace and allowed continuation\n")
		unix.Write(p2c[1], []byte("K"))
    

		

		// wait for EOF on pipe
		buf = make([]byte, 1)
		_, _ = unix.Read(p2c[0], buf)
		unix.Close(p2c[0])

		// reap child
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	}
}
