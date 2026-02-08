package sup

import (
	"fmt"
	"os"

	"github.com/Johnermac/bctor/lib"
	"golang.org/x/sys/unix"
)

func RunContainerInit(ctx lib.SupervisorCtx, spec *lib.ContainerSpec) {
	unix.Close(ctx.P2C[1]) // child reads only
	unix.Close(ctx.C2P[0]) // child writes only

	if spec.ShareNetNS != nil {		    
    spec.ShareNetNS.FD = lib.RecvNetNSFD(ctx)
		fmt.Printf("--[>] Init: Received shared netns fd=%d from supervisor\n", spec.ShareNetNS.FD)
	}


	os.Stdout.WriteString("--[*] Init: Start to Apply Namespaces\n")
	err := lib.ApplyNamespaces(spec)
	if err != nil {
		os.Stdout.WriteString("--[?] Error while applying NS: " + err.Error() + "\n")
		unix.Exit(1)
	}		

	if spec.Namespaces.AnyEnabled() {
		os.Stdout.WriteString("\n--[*] PARENT-CHILD\n")
		lib.LogNamespace(ctx.ParentNS, os.Getpid())
	}


	// PIPE HANDSHAKE

	fmt.Println("\n--[1] Init: pipe handshake - waiting for parent")
	// 1. signal parent: "I'm alive"
	if _, err := unix.Write(ctx.C2P[1], []byte{1}); err != nil {
		panic("--[?] handshake: failed to notify parent")
	}

	// 2. wait for parent to finish userns setup
	buf := make([]byte, 1)
	if _, err := unix.Read(ctx.P2C[0], buf); err != nil {
		panic("--[?] handshake: failed to receive continue signal")
	}

	fmt.Println("--[4] Init: Finished handshake, continuing with setup")

	// CONTROLS

	if spec.Namespaces.CGROUP {
		os.Stdout.WriteString("--[*] Init: CGroup\n")
		lib.SetupCgroups(spec.Namespaces.CGROUP, spec.Cgroups)
	}

	if spec.Namespaces.MOUNT {

		pid, err := lib.NewFork()
		if err != nil {
			os.Stdout.WriteString("--[?] Fork failed: " + err.Error() + "\n")
			return
		}

		lib.SetupRootAndSpawnWorkload(
			spec,
			pid,			
			ctx)
	}
}


