package main

import (
	"log"
	"os"
	"runtime"
	"strconv"

	"github.com/Johnermac/bctor/lib"
	"golang.org/x/sys/unix"
)

type ContainerSpec struct {
	ID           string
	Namespaces   lib.NamespaceConfig
	FS           lib.FSConfig
	Capabilities lib.CapsConfig
	Cgroups      lib.CGroupsConfig // nil = disabled
	Seccomp      lib.Profile
	Workload     lib.WorkloadSpec
}

type SupervisorCtx struct {
	ParentNS *lib.NamespaceState
	P2C      [2]int
	C2P      [2]int
	ChildPID uintptr
}

type ContainerState int

const (
    ContainerCreated ContainerState = iota
    ContainerRunning
    ContainerExited
)

type Container struct {
    Spec  ContainerSpec
    PID   int
    State ContainerState
}


var ctx SupervisorCtx
var spec ContainerSpec

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx := runSupervisor(ctx)	

	c, err := StartContainer(DefaultShellSpec(), ctx)
	if err != nil {
		log.Fatal(err)
	}

	containers := make(map[string]*Container)
	containers[c.Spec.ID] = c

	// reap child
	var status unix.WaitStatus
	_, _ = unix.Wait4(int(ctx.ChildPID), &status, 0, nil)
	c.State = ContainerExited

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

	return ctx

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
}

func StartContainer(spec ContainerSpec, ctx SupervisorCtx) (*Container, error) {
	var err error
	ctx.ChildPID, err = lib.NewFork()
	if err != nil {
		return nil, err
	}

	if ctx.ChildPID == 0 {
		runContainerInit(ctx)
	} else {
		runSupervisorLoop(ctx)
	}

	c := &Container{
		Spec:  spec,
		PID:   int(ctx.ChildPID),
		State: ContainerRunning,
	}

  return c, nil
}

func DefaultShellSpec() ContainerSpec {
	spec.Namespaces = lib.NamespaceConfig{
		USER:  true, //almost everything needs this enabled
		MOUNT: true,
		//CGROUP: true, //needs root cause /sys/fs/cgroup
		//PID: true,
		//UTS: true,
		//NET: true,
		//IPC: true,
	}

	spec.FS = lib.FSConfig{
			Rootfs:   "/dev/shm/bctor-root/",
			ReadOnly: false, // no permission, debug later
			Proc:     true,
			Sys:      true,
			Dev:      true,
			UseTmpfs: true,
		}

	spec.Cgroups = lib.CGroupsConfig{
		Path:      "/sys/fs/cgroup/bctor",
		CPUMax:    "50000 100000", // 50% CPU
		MemoryMax: "12M",
		PIDsMax:   "5",
	}

	spec.Capabilities = lib.CapsConfig{
			AllowCaps: []lib.Capability{lib.CAP_NET_BIND_SERVICE},			
	}

	return spec
}

func runContainerInit(ctx SupervisorCtx) {
	unix.Close(ctx.P2C[1])
	unix.Close(ctx.C2P[0])	

	os.Stdout.WriteString("[*] Apply Namespaces")
	err := lib.ApplyNamespaces(spec.Namespaces)
	if err != nil {
		os.Stdout.WriteString("Error while applying NS: " + err.Error() + "\n")
		unix.Exit(1)
	}

	if spec.Namespaces.AnyEnabled() {
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

	if spec.Namespaces.CGROUP {
		os.Stdout.WriteString("[*] CGroup\n")
		lib.SetupCgroups(spec.Namespaces.CGROUP, spec.Cgroups)
	}		

	if spec.Namespaces.MOUNT {		

		pid, err := lib.NewFork()
		if err != nil {
			os.Stdout.WriteString("Fork failed: " + err.Error() + "\n")
			return
		}

		lib.SetupRootAndSpawnWorkload(spec.FS, pid, spec.Capabilities, ctx.ParentNS)
	}
}