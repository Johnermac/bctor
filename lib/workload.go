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
	ctx SupervisorCtx) {

	fmt.Printf("--[!] Init: Second fork done\n")
	if pid == 0 {
		unix.Close(ctx.Init2sup[0])

		fmt.Println("---[*] Workload: File System Setup")
		FileSystemSetup(spec.FS)

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
		fmt.Printf(
			"--[DBG] Init: container-init: PID=%d waiting for child PID=%d\n",
			os.Getpid(), pid,
		)
		// notify supervisor of workload PID
		initWorkloadHandling(spec, int(pid), ctx)
	}
}

func initWorkloadHandling(spec *ContainerSpec, workloadPID int, ctx SupervisorCtx) {		

	if spec.Namespaces.NET {
		// creator container
		netnsFD, err := unix.Open("/proc/self/ns/net", unix.O_PATH, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--[?] Failed to open netns fd: %v\n", err)
			return 
		}

		fmt.Printf("--[>] Init: Sending netns fd=%d and workload PID=%d to supervisor\n",
			netnsFD, workloadPID,
		)
		SendWorkloadPIDAndNetNS(ctx, workloadPID, netnsFD)
	} else {
		// joining container
		fmt.Printf("--[>] Init: Sending workload PID=%d to supervisor\n", workloadPID)
		SendWorkloadPID(ctx, workloadPID)
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