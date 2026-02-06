package lib

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type WorkloadSpec struct {
	Path string // absolute inside container (/bin/sh, /bin/nc, etc)
	Args []string
	Env  []string
}

var WorkloadRegistry = map[Profile]WorkloadSpec{
	ProfileDebugShell: {
		Path: "/bin/sh",
		Args: []string{"sh"},
		Env:  []string{"PATH=/bin"},
	},
	ProfileWorkload: {
		Path: "/bin/nc",
		Args: []string{"nc", "-lp", "80"},
		Env:  os.Environ(),
	},
}

func SetupRootAndSpawnWorkload(
	fsCfg FSConfig,
	pid uintptr,
	cfg CapsConfig,
	p *NamespaceState,
	init2sup [2]int) {

	if pid == 0 {
		unix.Close(init2sup[0])

		fmt.Println("[*] File System Setup")
		FileSystemSetup(fsCfg)

		os.Stdout.WriteString("[*] Applying Capabilities isolation\n")
		SetupCapabilities(cfg)

		fmt.Println("[*] Run Workload")
		runWorkload()

		// If we get here, exec failed
		fmt.Fprintln(os.Stderr, "[?] workload returned unexpectedly")
		unix.Exit(127)

	} else {
		fmt.Printf(
			"[DBG] container-init: PID=%d waiting for child PID=%d\n",
			os.Getpid(), pid,
		)
		// notify supervisor of workload PID
		subReaper(int(pid), init2sup)
	}
}

func runWorkload() {

	profile := ProfileHello //set to arg in the future
	fmt.Println("[*] Apply Seccomp Profile:", profile)
	err := ApplySeccomp(profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ApplySeccomp failed:", err)
	}

	if profile == ProfileHello {
		syscall.Write(1, []byte("\n[!] Hello Seccomp!\n"))
		syscall.Exit(0)
	}

	if spec, ok := WorkloadRegistry[profile]; ok {
		fmt.Printf("[*] Executing: %s %v\n", spec.Path, spec.Args)

		err := unix.Exec(spec.Path, spec.Args, spec.Env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Exec failed for %s: %v\n", spec.Path, err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintln(os.Stderr, "No workload spec found for this profile")
		os.Exit(1)
	}

	// unreachable
	unix.Exit(0)
}

func subReaper(workloadPID int, init2sup [2]int) {
	fmt.Printf("[*] container-init > Send workload PID: %d\n", workloadPID)

	// notify supervisor of workload PID
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(workloadPID))
	_, _ = unix.Write(init2sup[1], buf)
	unix.Close(init2sup[1])

	var status unix.WaitStatus
	wpid, err := unix.Wait4(workloadPID, &status, 0, nil)

	fmt.Printf(
		"[DBG] container-init wait returned: wpid=%d exited=%v signaled=%v status=%v err=%v\n",
		wpid,
		status.Exited(),
		status.Signaled(),
		status,
		err,
	)

	if status.Exited() {
		fmt.Printf("[DBG] container-init exiting with %d\n", status.ExitStatus())
		os.Exit(status.ExitStatus())
	}
	if status.Signaled() {
		fmt.Printf("[DBG] container-init exiting via signal %d\n", status.Signal())
		os.Exit(128 + int(status.Signal()))
	}

	fmt.Println("[DBG] container-init exiting cleanly")
	os.Exit(0)
}
