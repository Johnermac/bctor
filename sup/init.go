package sup

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"golang.org/x/sys/unix"
)

func RunContainerInit(
	scx *lib.SupervisorCtx,
	spec *lib.ContainerSpec,
	ipc *lib.IPC) {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	os.Stdout.WriteString("--[*] Init: Start to Apply Namespaces\n")
	if err := lib.ApplyNamespaces(spec, ipc); err != nil {
		fmt.Fprintf(os.Stderr, "--[?] Init: Failed to apply namespaces: %v\n", err)		
		fmt.Fprintf(os.Stderr, "--[!] Init: full spec: %+v\n", spec.Namespaces)
		os.Exit(1)
	}

	if spec.Namespaces.AnyEnabled() {
		os.Stdout.WriteString("\n--[*] PARENT-CHILD\n")
		lib.LogNamespace(scx.ParentNS, os.Getpid())
	}

	// CONTROLS

	if spec.Namespaces.CGROUP {
		os.Stdout.WriteString("--[*] Init: CGroup\n")
		lib.SetupCgroups(spec.Namespaces.CGROUP, spec.Cgroups)
	}

	pid, err := lib.NewFork()
	if err != nil {
		os.Stdout.WriteString("--[?] Fork failed: " + err.Error() + "\n")
		return
	}

	lib.SetupRootAndSpawnWorkload(
		spec,
		pid,
		ipc)

	unix.Close(ipc.Init2Sup[1])
}
