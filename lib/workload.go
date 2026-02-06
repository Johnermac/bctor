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
		workloadPID := int(pid)
		fmt.Printf("[*] container-init > Send workload PID: %d\n", workloadPID)

		// notify supervisor of workload PID
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(workloadPID))
		_, _ = unix.Write(init2sup[1], buf)
		unix.Close(init2sup[1])

		fmt.Printf("[*] var status unix.WaitStatus\n")
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)

		if status.Exited() {
			os.Exit(status.ExitStatus())
		}
		if status.Signaled() {
			os.Exit(128 + int(status.Signal()))
		}

		os.Exit(0)

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
