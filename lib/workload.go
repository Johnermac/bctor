package lib

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func setupBatchIO(ipc *IPC) {
	// Redirect stdout/stderr to pipe
	unix.Dup2(ipc.Log2Sup[1], 1)
	unix.Dup2(ipc.Log2Sup[1], 2)

	// Close unused ends
	unix.Close(ipc.Log2Sup[0])
	unix.Close(ipc.Log2Sup[1])
}

func setupInteractiveIO(ipc *IPC) {
	sFd := int(ipc.PtySlave.Fd())
	mFd := int(ipc.PtyMaster.Fd())

	// New session and controlling TTY for interactive shell.
	_, _ = unix.Setsid()
	_ = unix.IoctlSetPointerInt(sFd, unix.TIOCSCTTY, 0)

	_ = unix.Dup2(sFd, 0)
	_ = unix.Dup2(sFd, 1)
	_ = unix.Dup2(sFd, 2)

	unix.SetNonblock(0, false)
	unix.SetNonblock(1, false)
	unix.SetNonblock(2, false)

	// Close originals
	if mFd > 2 {
		unix.Close(mFd)
	}
	if sFd > 2 {
		unix.Close(sFd)
	}
}

func setupIO(spec *ContainerSpec, ipc *IPC) {
	switch spec.Workload.Mode {
	case ModeInteractive:
		setupInteractiveIO(ipc)
	case ModeBatch:
		setupBatchIO(ipc)
	}
}

func SetupRootAndSpawnWorkload(
	spec *ContainerSpec,
	pid uintptr,
	ipc *IPC) {

	if pid == 0 {

		// SETUP IO BY MODE
		//fmt.Printf("MODE: %v\n", spec.Workload.Mode)
		setupIO(spec, ipc)

		if spec.Namespaces.MOUNT && !specJoinsNamespace(spec, NSMnt) {
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
			LogInfo("Workload: mount setup already shared, skipping rootfs/pivot setup")
		}

		if spec.Namespaces.MOUNT && !specJoinsNamespace(spec, NSMnt) {
			if err := MountVirtualFS(spec.FS); err != nil {
				LogError("Error MountVirtualFS")
				unix.Exit(1)
			}
		}

		LogInfo("Workload: Applying Capabilities isolation")
		SetupCapabilities(spec.Capabilities)
		//DebugMountContext()

		// SECCOMP

		if spec.IsNetRoot {
			//LogInfo("PAUSE")
			WaitFd(ipc.NetReady[0])
		}

		//fmt.Println("---[*] Workload: Run Workload")

		runWorkload(spec)

		// If we get here, exec failed
		LogError("Workload: Returned unexpectedly")
		unix.Exit(127)

	} else {
		unix.Close(ipc.Log2Sup[1])
		unix.Close(ipc.Log2Sup[0])

		// notify supervisor of workload PID
		initWorkloadHandling(spec, int(pid), ipc)
	}
}

func runWorkload(spec *ContainerSpec) {
	profile := spec.Seccomp
	if _, ok := WorkloadRegistry[profile]; !ok {
		if spec.Workload.Mode == ModeBatch {
			profile = ProfileBatch
		} else {
			profile = ProfileDebugShell
		}
	}

	wspec, ok := WorkloadRegistry[profile]
	if !ok {
		unix.Exit(1)
	}

	// Exec replaces current process
	if err := unix.Exec(wspec.Path, wspec.Args, wspec.Env); err != nil {
		fmt.Fprintf(os.Stderr, "exec failed: %v\n", err)
		unix.Exit(127)
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

func specJoinsNamespace(spec *ContainerSpec, ns NamespaceType) bool {
	for _, s := range spec.Shares {
		if s.Type == ns {
			return true
		}
	}
	return false
}
