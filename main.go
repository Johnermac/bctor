package main

import (
	"os"
	"runtime"
	"strconv"

	"github.com/Johnermac/bctor/lib"
	"golang.org/x/sys/unix"
)

type SupervisorCtx struct {
	ParentNS *lib.NamespaceState
	P2C      [2]int
	C2P      [2]int
	ChildPID uintptr
}

var ctx SupervisorCtx

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx := runSupervisor(ctx)

	if ctx.ChildPID == 0 {

		runContainerInit(ctx)

	} else {

		runSupervisorLoop(ctx)
	}
}

func runSupervisor(ctx SupervisorCtx) SupervisorCtx {

	// fd[0] is the read end, fd[1] is the write end
	err := unix.Pipe2(ctx.P2C[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	err = unix.Pipe2(ctx.C2P[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	ctx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	ctx.ChildPID, err = lib.NewFork()
	if err != nil {
		os.Stdout.WriteString("[?] fork failed: " + err.Error() + "\n")
		panic(err)
	}

	return ctx

}

func runContainerInit(ctx SupervisorCtx) {
	unix.Close(ctx.P2C[1])
	unix.Close(ctx.C2P[0])

	cfg := lib.NamespaceConfig{
		USER:  true, //almost everything needs this enabled
		MOUNT: true,
		//CGROUP: true, //needs root cause /sys/fs/cgroup
		//PID: true,
		//UTS: true,
		//NET: true,
		//IPC: true,
	}

	os.Stdout.WriteString("[*] Apply Namespaces")
	err := lib.ApplyNamespaces(cfg)
	if err != nil {
		os.Stdout.WriteString("Error while applying NS: " + err.Error() + "\n")
		unix.Exit(1)
	}

	if cfg.AnyEnabled() {
		os.Stdout.WriteString("\n[*] Compare Namespaces PARENT-CHILD\n")
		childNS, _ := lib.ReadNamespaces(os.Getpid())
		nsdiff := lib.DiffNamespaces(ctx.ParentNS, childNS)
		lib.LogNamespaceDelta(nsdiff)
	}

	// PIPE HANDSHAKE

	os.Stdout.WriteString("\n[*] 1 - pipe handshake started with parent\n")
	unix.Write(ctx.C2P[1], []byte("G"))

	buf := make([]byte, 1)
	unix.Read(ctx.P2C[0], buf)

	os.Stdout.WriteString("[*] 4 - finished like chads\n")

	// CONTROLS

	if cfg.CGROUP {
		os.Stdout.WriteString("[*] CGroup\n")
		lib.SetupCgroups(cfg)
	}

	if cfg.PID {
		os.Stdout.WriteString("\n[*] Compare Namespaces PARENT-GRANDCHILD\n")
		lib.ValidatePIDNamespace(ctx.ParentNS, cfg)
	}

	os.Stdout.WriteString("[*] Drop Capabilities\n")
	lib.SetupCapabilities()

	if cfg.MOUNT {
		os.Stdout.WriteString("[*] File System setup\n")
		fsCfg := lib.FSConfig{
			Rootfs:   "/dev/shm/bctor-root/",
			ReadOnly: false, // no permission, debug later
			Proc:     true,
			Sys:      true,
			Dev:      true,
			UseTmpfs: true,
		}

		pid, err := lib.NewFork()
		if err != nil {
			os.Stdout.WriteString("Fork failed: " + err.Error() + "\n")
			return
		}

		lib.SetupRootAndSpawnWorkload(fsCfg, pid)
	}
}

func runSupervisorLoop(ctx SupervisorCtx) {
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

	// reap child
	var status unix.WaitStatus
	_, _ = unix.Wait4(int(ctx.ChildPID), &status, 0, nil)
}
