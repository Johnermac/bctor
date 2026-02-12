package lib

import (
	"fmt"
	"net"
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

		profile := ProfileLs

		/*fmt.Println("[*] Apply Seccomp Profile:", profile)
		err := ApplySeccomp(profile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ApplySeccomp failed:", err)
		}*/

		if spec.IsNetRoot {
			//LogInfo("PAUSE")
			WaitFd(ipc.NetReady[0])
		}

		fmt.Println("---[*] Workload: Run Workload")
		runWorkload(profile)

		// If we get here, exec failed
		fmt.Fprintln(os.Stderr, "---[?] Workload: Returned unexpectedly")
		unix.Exit(127)

	} else {

		fmt.Printf("--[!] Init: Fork() Container-init -> Workload\n")
		fmt.Printf(
			"--[DBG] Init: container-init: PID=%d waiting for child PID=%d\n",
			os.Getpid(), pid,
		)
		// notify supervisor of workload PID
		initWorkloadHandling(spec, int(pid), ipc)
	}
}

func initWorkloadHandling(spec *ContainerSpec, workloadPID int, ipc *IPC) {
	// 1. Always send workload PID
	SendWorkloadPID(ipc, workloadPID)

	// 2. Collect namespace FDs this container CREATED
	handles := CollectCreatedNamespaceFDs(spec)
	if len(handles) == 0 {
		return
	}

	// 3. Send namespace handles to supervisor
	SendCreatedNamespaceFDs(ipc, handles)

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

	//os.Stdout.Sync()
	//os.Stderr.Sync()

	if profile == ProfileHello {
		syscall.Write(1, []byte("\n---[!] EXEC: Hello Seccomp!\n"))
		syscall.Exit(0)
	}

	if profile == ProfileNetworkVerify {
		fmt.Println("---[*] Workload: Native Network Verification")

		interfaces, err := net.Interfaces()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		for _, iface := range interfaces {
			addrs, _ := iface.Addrs()
			fmt.Printf("  -> Interface: %s | MTU: %d | Addrs: %v\n",
				iface.Name, iface.MTU, addrs)
		}
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
