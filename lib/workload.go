package lib

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)



func SetupRootAndSpawnWorkload(
	spec *ContainerSpec,
	pid uintptr,
	ipc *IPC) {

	
	if pid == 0 {	
		
		if spec.Namespaces.MOUNT {
			fmt.Println("---[*] Workload: File System Setup")
			FileSystemSetup(spec.FS)
		}
		
		os.Stdout.WriteString("---[*] Workload: Applying Capabilities isolation\n")
		SetupCapabilities(spec.Capabilities)

		profile := ProfileIpLink //set to arg in the future
		/*fmt.Println("[*] Apply Seccomp Profile:", profile)
		err := ApplySeccomp(profile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ApplySeccomp failed:", err)
		}*/

		fmt.Println("---[*] Workload: Run Workload")
		runWorkload(profile)

		// If we get here, exec failed
		fmt.Fprintln(os.Stderr, "---[?] Workload: Returned unexpectedly")
		unix.Exit(127)

	} else {
		fmt.Printf("--[!] Init: Second fork done\n")
		fmt.Printf(
			"--[DBG] Init: container-init: PID=%d waiting for child PID=%d\n",
			os.Getpid(), pid,
		)
		// notify supervisor of workload PID
		initWorkloadHandling(spec, int(pid), ipc)
	}	
}

func initWorkloadHandling(spec *ContainerSpec, workloadPID int, ipc *IPC) {
	if spec.Namespaces.NET {
		// creator container

		usernsFD, err := unix.Open("/proc/self/ns/user", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--[?] Failed to open userns fd: %v\n", err)
			return
		}

		netnsFD, err := unix.Open("/proc/self/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--[?] Failed to open netns fd: %v\n", err)
			unix.Close(usernsFD)
			return
		}

		fmt.Printf(
			"--[>] Init: Sending workload PID=%d userns fd=%d netns fd=%d to supervisor\n",
			workloadPID, usernsFD, netnsFD,
		)

		if err := SendWorkPIDUserNetNS(ipc, workloadPID, usernsFD, netnsFD); err != nil {
			fmt.Fprintf(os.Stderr, "--[?] Failed to send workload PID + namespace fds: %v\n", err)
		}

		unix.Close(usernsFD)
		unix.Close(netnsFD)
	} else {
		// joining container
		fmt.Printf("--[>] Init: Sending workload PID=%d to supervisor\n", workloadPID)
		SendWorkloadPID(ipc, workloadPID)
	}

	var status unix.WaitStatus
	wpid, _ := unix.Wait4(workloadPID, &status, 0, nil)

	fmt.Printf(
		"--[DBG] Init: Wait returned: wpid=%d exited=%v signaled=%v status=%v\n",
		wpid,
		status.Exited(),
		status.Signaled(),
		status,
	)	

	if status.Exited() {
		fmt.Printf("--[DBG] Init: Exiting with %d\n", status.ExitStatus())
		os.Exit(status.ExitStatus())
	}
	if status.Signaled() {
		fmt.Printf("--[DBG] Init: Exiting via signal %d\n", status.Signal())
		os.Exit(128 + int(status.Signal()))
	}

	fmt.Println("--[DBG] Init: Exiting cleanly")
	os.Exit(0)
}

func runWorkload(profile Profile) {	

	if profile == ProfileHello {
		syscall.Write(1, []byte("\n---[!] EXEC: Hello Seccomp!\n"))
		syscall.Exit(0)
	}

	if spec, ok := WorkloadRegistry[profile]; ok {
		fmt.Printf("---[*] Executing: %s %v\n", spec.Path, spec.Args)

		err := unix.Exec(spec.Path, spec.Args, spec.Env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "---[?] Exec failed for %s: %v\n", spec.Path, err)
			os.Exit(1)
		}		
	} else {
		fmt.Fprintln(os.Stderr, "---[?] No workload spec found for this profile")
		os.Exit(1)
	}	

	// unreachable
	unix.Exit(0)
}