package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/signal"
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
	Init2sup [2]int
	ChildPID uintptr
}

type ContainerState int

const (
    ContainerCreated ContainerState = iota
    ContainerRunning
		ContainerStopped
    ContainerExited
)

type Container struct {
    Spec  			*ContainerSpec
    InitPID   	int
		WorkloadPID int
    State 		  ContainerState
}






var ctx SupervisorCtx
var spec ContainerSpec


func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx := runSupervisor(ctx)

	sigCh := signalHandling()

	c, err := StartContainer(DefaultShellSpec(), ctx)
	if err != nil {
		log.Fatal(err)
	}

	containers := make(map[string]*Container)
	containers[c.Spec.ID] = c

	fmt.Printf("[!] initPID: %d\n", c.InitPID)
	fmt.Printf("[!] WorkloadPID: %d\n", c.WorkloadPID)
			

	startSignalForwarder(containers, sigCh)
	reapChildren(containers)		
	
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

	err = unix.Pipe2(ctx.Init2sup[:], unix.O_CLOEXEC)
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

	containers := map[string]*Container{}
	containers[spec.ID] = &Container{
		Spec:  &spec,
		InitPID:   int(ctx.ChildPID),
		State: ContainerCreated,
	}


	if ctx.ChildPID == 0 {
		runContainerInit(ctx)
	} else {
		runSupervisorLoop(ctx)
	}

	buf := make([]byte, 8)
	unix.Read(ctx.Init2sup[0], buf)
	workloadPID := int(binary.LittleEndian.Uint64(buf))

	c := &Container{
		Spec:  &spec,
		InitPID:   int(ctx.ChildPID),
		WorkloadPID: workloadPID,
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

		lib.SetupRootAndSpawnWorkload(
			spec.FS, 
			pid, 
			spec.Capabilities, 
			ctx.ParentNS, 
			ctx.Init2sup)
	}
}

func waitForAnyChild() (int, unix.WaitStatus, error) {
    var status unix.WaitStatus
    pid, err := unix.Wait4(-1, &status, 0, nil)
    if err != nil {
        return -1, status, err
    }
    return pid, status, nil
}

func findContainerByPID(containers map[string]*Container, pid int) *Container {
    for _, c := range containers {
        if c.WorkloadPID == pid || c.InitPID == pid {
            return c
        }
    }
    return nil
}

func signalHandling() chan os.Signal {
	sigCh := make(chan os.Signal, 16)
	signal.Notify(
    sigCh,
    unix.SIGINT,
    unix.SIGTERM,
    unix.SIGQUIT,
    unix.SIGHUP,
	)
	return sigCh
}

func reapChildren(containers map[string]*Container) {
	for len(containers) > 0 {
    pid, _, err := waitForAnyChild()
    if err != nil {
        if err == unix.EINTR {
            continue
        }
        break
    }

    c := findContainerByPID(containers, pid)
    if c == nil {
			// orphan or unknown child; still must reap
			continue
    }		

		if pid == c.WorkloadPID {
			c.State = ContainerExited
			delete(containers, c.Spec.ID)
    }
	}
}

func startSignalForwarder(
    containers map[string]*Container,
    sigCh chan os.Signal,
) {
    go func() {
        for sig := range sigCh {
            for _, c := range containers {
                if c.WorkloadPID > 0 {
                    _ = unix.Kill(c.WorkloadPID, sig.(unix.Signal))
                }
            }
        }
    }()
}
