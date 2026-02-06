package sup

import (
	"os"
	"strconv"

	"github.com/Johnermac/bctor/lib"

	"golang.org/x/sys/unix"
)

func RunContainerInit(ctx lib.SupervisorCtx) {
	unix.Close(ctx.P2C[1])
	unix.Close(ctx.C2P[0])

	os.Stdout.WriteString("[*] Apply Namespaces")
	err := lib.ApplyNamespaces(lib.DefaultShellSpec().Namespaces)
	if err != nil {
		os.Stdout.WriteString("Error while applying NS: " + err.Error() + "\n")
		unix.Exit(1)
	}

	if lib.DefaultShellSpec().Namespaces.AnyEnabled() {
		os.Stdout.WriteString("\n[*] PARENT-CHILD\n")
		lib.LogNamespace(ctx.ParentNS, os.Getpid())
	}

	// PIPE HANDSHAKE

	os.Stdout.WriteString("\n[*] 1 - pipe handshake started with parent\n")
	unix.Write(ctx.C2P[1], []byte("G"))

	buf := make([]byte, 1)
	unix.Read(ctx.P2C[0], buf)

	os.Stdout.WriteString("[*] 4 - finished like chads\n")

	// CONTROLS

	if lib.DefaultShellSpec().Namespaces.CGROUP {
		os.Stdout.WriteString("[*] CGroup\n")
		lib.SetupCgroups(lib.DefaultShellSpec().Namespaces.CGROUP, lib.DefaultShellSpec().Cgroups)
	}

	if lib.DefaultShellSpec().Namespaces.MOUNT {

		pid, err := lib.NewFork()
		if err != nil {
			os.Stdout.WriteString("Fork failed: " + err.Error() + "\n")
			return
		}

		lib.SetupRootAndSpawnWorkload(
			lib.DefaultShellSpec().FS,
			pid,
			lib.DefaultShellSpec().Capabilities,
			ctx.ParentNS,
			ctx.Init2sup)
	}
}

func PipeHandshake(ctx lib.SupervisorCtx) {
	unix.Close(ctx.P2C[0]) // Pai só escreve no p2c
	unix.Close(ctx.C2P[1]) // Pai só lê do c2p

	// 1. Espera o Filho avisar que nasceu
	buf := make([]byte, 1)
	unix.Read(ctx.C2P[0], buf)

	os.Stdout.WriteString("[*] 2 - ok buddy\n")

	pidStr := strconv.Itoa(int(ctx.ChildPID)) //child pid

	if err := lib.SetupUserNamespace(pidStr); err != nil {
		os.Stdout.WriteString("[?] X - Error SetupUserNamespace: " + err.Error() + "\n")
		unix.Exit(1)
	}

	os.Stdout.WriteString("[*] 3 - parent set up user namespace and allowed continuation\n")
	unix.Write(ctx.P2C[1], []byte("K"))

	// wait for EOF on pipe
	buf = make([]byte, 1)
	_, _ = unix.Read(ctx.P2C[0], buf)
	unix.Close(ctx.P2C[0])
}
