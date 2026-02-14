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
    unix.Dup2(ipc.Log2Sup[1], 1) // Redirect Stdout
    unix.Dup2(ipc.Log2Sup[1], 2) // Redirect Stderr
   
    unix.Close(ipc.Log2Sup[0])
    unix.Close(ipc.Log2Sup[1])

		if spec.Namespaces.MOUNT {			
			if err := PrepareRoot(spec.FS); err != nil {
				LogError("prepare")
				unix.Exit(1)
			}
			if err := PivotRoot(spec.FS.Rootfs); err != nil {
				LogError("pivotroot")
				unix.Exit(1)
			}

			os.MkdirAll("/proc", 0555)
			os.MkdirAll("/sys", 0555)
		} else {			
			LogInfo("Workload: Joiner detected, skipping PivotRoot")
		}
		
		if err := MountVirtualFS(spec.FS); err != nil {
			LogError("Error MountVirtualFS")
			unix.Exit(1)
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
		unix.Close(ipc.Log2Sup[1])
		unix.Close(ipc.Log2Sup[0])
    

		//fmt.Printf("Init: Fork() Container-init -> Workload\n")
		//fmt.Printf("Init: container-init: PID=%d waiting for child PID=%d\n",os.Getpid(), pid)
		// notify supervisor of workload PID
		initWorkloadHandling(spec, int(pid), ipc)
	}
}

func initWorkloadHandling(spec *ContainerSpec, workloadPID int, ipc *IPC) {

	SendWorkloadPID(ipc, workloadPID)
	handles := CollectCreatedNamespaceFDs(spec)
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

	// 2. ONLY the Creator/Root needs to hold the door open
	if spec.IsNetRoot {
		LogInfo("Init: %s Workload done, holding namespaces for joiners...", spec.ID)
		
		unix.Close(ipc.KeepAlive[1])		
		WaitFd(ipc.KeepAlive[0])

		LogInfo("Init: %s Released by Supervisor", spec.ID)
	} else {
		// Joiners can exit immediately after their workload is done
		LogInfo("Init: %s Workload done, exiting joiner", spec.ID)
	}

	// 3. Final Exit based on the status captured BEFORE the wait
	if status.Exited() {
		os.Exit(status.ExitStatus())
	}
	if status.Signaled() {
		os.Exit(128 + int(status.Signal()))
	}
	os.Exit(0)
}

func runWorkload(spec *ContainerSpec, ipc *IPC) {
    
    spec.Seccomp = ProfileIpLink
    wspec, ok := WorkloadRegistry[spec.Seccomp]
    if !ok {
        os.Exit(1)
    }    

    cmd := exec.Command(wspec.Path, wspec.Args[1:]...)
    cmd.Env = wspec.Env
    
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    
    if err := cmd.Run(); err != nil {        
        fmt.Fprintf(os.Stderr, "Workload execution failed: %v\n", err)
    }
    
    os.Exit(0)
}
