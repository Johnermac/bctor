package lib

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func SetupRootAndSpawnWorkload(
	spec *ContainerSpec,
	pid uintptr,
	ipc *IPC) {

	if pid == 0 {
		//workload	
		//DebugMountContext()	

		if spec.Namespaces.MOUNT {
			LogInfo("Workload: File System Setup")
			FileSystemSetup(spec.FS)			
		}

		LogInfo("Workload: Applying Capabilities isolation")
		SetupCapabilities(spec.Capabilities)
		//DebugMountContext()

		

		/*fmt.Println("[*] Apply Seccomp Profile:", profile)
		err := ApplySeccomp(profile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ApplySeccomp failed:", err)
		}*/

		if spec.IsNetRoot {
			//LogInfo("PAUSE")
			WaitFd(ipc.NetReady[0])
		}

		//fmt.Println("---[*] Workload: Run Workload")
		
		runWorkload(spec, ipc)

		// If we get here, exec failed
		LogError("Workload: Returned unexpectedly")
		unix.Exit(127)

	} else {

		//fmt.Printf("Init: Fork() Container-init -> Workload\n")
		//fmt.Printf("Init: container-init: PID=%d waiting for child PID=%d\n",os.Getpid(), pid)
		// notify supervisor of workload PID
		initWorkloadHandling(spec, int(pid), ipc)
	}
}

func initWorkloadHandling(spec *ContainerSpec, workloadPID int, ipc *IPC) {
	// 1. Always send workload PID
	SendWorkloadPID(ipc, workloadPID)

	// 2. Collect namespace FDs this container CREATED
	handles := CollectCreatedNamespaceFDs(spec)
	//if len(handles) == 0 { return	}

	// 3. Send namespace handles to supervisor
	SendCreatedNamespaceFDs(ipc, handles)

	var status unix.WaitStatus
	_, _ = unix.Wait4(workloadPID, &status, 0, nil)

	/*fmt.Printf(
		"Init: Wait returned: wpid=%d exited=%v signaled=%v status=%v\n",
		wpid,
		status.Exited(),
		status.Signaled(),
		status,
	)*/

	if status.Exited() {
		LogWarn("Init: Exiting %s with %d\n", spec.ID, status.ExitStatus())
		os.Exit(status.ExitStatus())
	}
	if status.Signaled() {
		LogWarn("Init: Exiting %s via signal %d\n", spec.ID, status.Signal())
		os.Exit(128 + int(status.Signal()))
	}

	LogWarn("Init: Exiting %s cleanly", spec.ID)
	os.Exit(0)
}

func runWorkload(spec *ContainerSpec, ipc *IPC) {
	//LogInfo("profile: %v", spec.Seccomp)
	spec.Seccomp = ProfileIpLink	
	
	wspec, ok := WorkloadRegistry[spec.Seccomp]
    if !ok { os.Exit(1) }

    // Close the read end inside the child (best practice)
    unix.Close(ipc.Log2Sup[1])

    cmd := exec.Command(wspec.Path, wspec.Args[1:]...)
    cmd.Env = wspec.Env

    // Convert the FD to a Go *os.File for the Cmd struct
    logFile := os.NewFile(uintptr(ipc.Log2Sup[0]), "log-socket")
    
    cmd.Stdout = logFile
    cmd.Stderr = logFile // Redirect both to the same socket

    // Run the process
    if err := cmd.Run(); err != nil {
        fmt.Fprintf(logFile, "Workload execution failed: %v\n", err)
    }

    // Explicitly close before exiting to signal EOF to Supervisor
    logFile.Close()
    os.Exit(0)
}