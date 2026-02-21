package sup

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"golang.org/x/sys/unix"
)

type appState struct {
	containers map[string]*Container
	scx        lib.SupervisorCtx
	mtx        *Multiplexer
	iface      string
	Forwards   map[string][]ntw.ForwardingSession
	LoggerDone chan bool
	LogChan    chan lib.LogMsg
	Wg         sync.WaitGroup
}

func Setup() (*appState, error) {
	state := &appState{
		containers: make(map[string]*Container),
		scx: lib.SupervisorCtx{
			Handles:  make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
			Forwards: ntw.NewPortForwarder(),
		},
		LoggerDone: make(chan bool),
		LogChan:    make(chan lib.LogMsg, 200),
	}

	events := make(chan Event, 32)
	StartReaper(events)
	StartSignalHandler(events)

	state.mtx = NewMultiplexer(state)
	go state.mtx.RunLoop()

	lib.GlobalLogChan = state.LogChan
	go lib.StartGlobalLogger(state.LogChan, state.LoggerDone, state.mtx)

	// prepare host
	ntw.EnableIPForwarding()
	iface, _ := ntw.DefaultRouteInterface()
	state.iface = iface
	ntw.AddNATRule("10.0.0.0/24", iface)

	// create "switch"
	if err := ntw.EnsureBridge("bctor0", "10.0.0.1/24"); err != nil {
		return nil, err
	}

	// dchp
	alloc, _ := ntw.NewIPAlloc("10.0.0.0/24")
	state.scx.IPAlloc = alloc
	state.scx.Subnet = alloc.Subnet
	state.scx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	state.Wg.Add(1) //onContainerExit
	go OnContainerExit(state.containers, &state.scx, events, state.iface, &state.Wg)
	//state.Wg.Add(1) // main loop

	return state, nil
}

func StartCreator(letter string, runMode lib.ExecutionMode, runProfile lib.Profile, state *appState, ipc *lib.IPC) (*Container, error) {

	// define config
	spec := lib.DefaultSpec()
	spec.ID = fmt.Sprintf("bctor-%s1", letter)
	spec.IsNetRoot = true
	spec.Workload.Mode = runMode
	spec.Seccomp = runProfile

	lib.LogInfo("Container %s=Creator", spec.ID)

	c, err := StartContainer(spec, state.LogChan, &state.scx, state.containers, ipc, &state.Wg)
	if err != nil {
		return nil, err
	}
	c.IPC = ipc

	if spec.Workload.Mode == lib.ModeBatch {
		readFd := ipc.Log2Sup[0]
		state.Wg.Add(1)
		go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.LogChan, &state.Wg)
	} else {
		unix.Close(ipc.Log2Sup[1])
	}

	ipc.PtySlave.Close()
	state.mtx.Register(spec.ID, ipc.PtyMaster, c.WorkloadPID)

	return c, nil
}

func StartJoiner(root *Container, name string, runMode lib.ExecutionMode, runProfile lib.Profile, state *appState, ipc *lib.IPC) {

	spec := lib.DefaultSpec()
	spec.Workload.Mode = runMode
	spec.ID = name
	spec.Seccomp = runProfile

	spec.Namespaces.USER = false
	spec.Namespaces.MOUNT = true
	spec.Namespaces.NET = true
	spec.Namespaces.PID = false
	spec.IsNetRoot = false

	spec.Shares = []lib.ShareSpec{
		{Type: lib.NSUser, FromContainer: root.Spec.ID},
		{Type: lib.NSNet, FromContainer: root.Spec.ID},
		{Type: lib.NSMnt, FromContainer: root.Spec.ID},
	}

	lib.LogInfo("Container %s=Joiner of: %s", spec.ID, root.Spec.ID)

	cj, err := StartContainer(spec, state.LogChan, &state.scx, state.containers, ipc, &state.Wg)
	if err != nil {
		lib.LogError("Start Joiner %s failed: %v", spec.ID, err)
		return
	}
	cj.IPC = ipc

	if ipc.PtyMaster == nil {
		lib.LogError("FATAL: PtyMaster for %s is nil", spec.ID)
		return
	}

	if spec.Workload.Mode == lib.ModeInteractive {
		unix.Close(ipc.Log2Sup[1])
	} else {
		readFd := ipc.Log2Sup[0]
		state.Wg.Add(1)
		go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.LogChan, &state.Wg)
	}

	state.mtx.Register(spec.ID, ipc.PtyMaster, cj.WorkloadPID)

	ipc.PtySlave.Close()
	lib.FreeFd(cj.IPC.KeepAlive[1])
	cj.IPC.KeepAlive[1] = -1
}

func (s *appState) GetNextPodLetter() (string, error) {
	s.scx.Mu.Lock()
	defer s.scx.Mu.Unlock()

	usedLetters := make(map[string]bool)
	for _, c := range s.containers {
		// If ID is "bctor-a1", podID is "a"
		parts := strings.Split(c.Spec.ID, "-")
		if len(parts) > 1 {
			podID := parts[1]
			letter := regexp.MustCompile(`[0-9]`).ReplaceAllString(podID, "")
			usedLetters[letter] = true
		}
	}

	// a to z for now- nobody will create more than 26 pods i guess
	for i := 0; i < 26; i++ {
		letter := string(rune('a' + i))
		if !usedLetters[letter] {
			return letter, nil
		}
	}

	return "", fmt.Errorf("alphabet exhausted: kill a pod to free up a letter")
}

func (s *appState) GetNextContainerIndex(letter string) int {
	s.scx.Mu.Lock()
	defer s.scx.Mu.Unlock()

	count := 0
	for id := range s.containers {
		// Check if ID contains "-a"
		if strings.Contains(id, "-"+letter) {
			count++
		}
	}
	return count + 1
}

func StartJoinerBatch(root *Container, name string, cmd string, state *appState, ipc *lib.IPC) {
	// normal joiner
	spec := lib.DefaultSpec()
	spec.ID = name

	// set to Batch Mode
	spec.Workload.Mode = lib.ModeBatch
	spec.Seccomp = lib.ProfileBatch
	spec.Workload.Args = []string{"sh", "-c", cmd}

	spec.Namespaces.USER = false
	spec.Namespaces.MOUNT = true
	spec.Namespaces.NET = true
	spec.Namespaces.PID = false
	spec.IsNetRoot = false

	spec.Shares = []lib.ShareSpec{
		{Type: lib.NSUser, FromContainer: root.Spec.ID},
		{Type: lib.NSNet, FromContainer: root.Spec.ID},
		{Type: lib.NSMnt, FromContainer: root.Spec.ID},
	}

	lib.LogInfo("[Batch] Container %s joining Pod %s: %s", spec.ID, root.Spec.ID, cmd)

	// Launch
	cj, err := StartContainer(spec, state.LogChan, &state.scx, state.containers, ipc, &state.Wg)
	if err != nil {
		lib.LogError("Start Batch Joiner %s failed: %v", spec.ID, err)
		return
	}
	cj.IPC = ipc

	// IO Handling in Batch mode, we use CaptureLogs to pipe output to the console
	if ipc.Log2Sup[1] != -1 {
		readFd := ipc.Log2Sup[0]
		state.Wg.Add(1)
		go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.LogChan, &state.Wg)
	}

	state.mtx.Register(spec.ID, ipc.PtyMaster, cj.WorkloadPID)

	// Cleanup FDs
	if ipc.PtySlave != nil {
		ipc.PtySlave.Close()
	}
	lib.FreeFd(cj.IPC.KeepAlive[1])
	cj.IPC.KeepAlive[1] = -1
}

func StartCreatorBatch(letter string, cmd string, state *appState, ipc *lib.IPC) (*Container, error) {
	// basic spec
	spec := lib.DefaultSpec()
	spec.ID = fmt.Sprintf("bctor-%s1", letter)
	spec.IsNetRoot = true
	spec.Workload.Mode = lib.ModeBatch
	spec.Seccomp = lib.ProfileBatch
	spec.Workload.Args = []string{"sh", "-c", cmd}

	lib.LogInfo("[Batch] Container %s=Creator (Command: %s)", spec.ID, cmd)

	// Launch
	c, err := StartContainer(spec, state.LogChan, &state.scx, state.containers, ipc, &state.Wg)
	if err != nil {
		return nil, err
	}
	c.IPC = ipc

	readFd := ipc.Log2Sup[0]
	state.Wg.Add(1)
	go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.LogChan, &state.Wg)

	// Cleanup and Register
	ipc.PtySlave.Close()
	state.mtx.Register(spec.ID, ipc.PtyMaster, c.WorkloadPID)

	lib.FreeFd(c.IPC.KeepAlive[1])
	c.IPC.KeepAlive[1] = -1

	return c, nil
}
