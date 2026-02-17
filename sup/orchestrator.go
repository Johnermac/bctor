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

	LoggerDone chan bool
	LogChan    chan lib.LogMsg
	Wg         sync.WaitGroup
}

func Setup(containerCount int) (*appState, error) {
	state := &appState{
		containers: make(map[string]*Container),
		scx: lib.SupervisorCtx{
			Handles: make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
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
	go OnContainerExit(state.containers, &state.scx, events, state.iface, &state.Wg, containerCount)
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
}

func (s *appState) GetNextPodLetter() (string, error) {
	s.scx.Mu.Lock()
	defer s.scx.Mu.Unlock()

	// 1. Create a set of letters currently in use by looking at active containers
	usedLetters := make(map[string]bool)
	for _, c := range s.containers {
		// If ID is "bctor-a1", podID is "a"
		// We extract the letter between the '-' and the number
		parts := strings.Split(c.Spec.ID, "-")
		if len(parts) > 1 {
			podID := parts[1]
			// Remove the number at the end (e.g., "a1" -> "a")
			letter := regexp.MustCompile(`[0-9]`).ReplaceAllString(podID, "")
			usedLetters[letter] = true
		}
	}

	// 2. Search from 'a' to 'z' for the first free letter
	for i := 0; i < 26; i++ {
		letter := string(rune('a' + i))
		if !usedLetters[letter] {
			return letter, nil
		}
	}

	return "", fmt.Errorf("alphabet exhausted: kill a pod to free up a letter")
}

func (s *appState) GetNextContainerIndex(letter string) int {
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
	// 1. Start with the same base as a normal joiner
	spec := lib.DefaultSpec()
	spec.ID = name

	// 2. Set to Batch Mode and use a basic Batch Profile (Seccomp/Capabilities)
	spec.Workload.Mode = lib.ModeBatch
	spec.Seccomp = lib.ProfileBatch
	spec.Workload.Args = []string{"sh", "-c", cmd}

	// 4. Namespace Sharing (Identical to your StartJoiner)
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

	// 5. Launch
	cj, err := StartContainer(spec, state.LogChan, &state.scx, state.containers, ipc, &state.Wg)
	if err != nil {
		lib.LogError("Start Batch Joiner %s failed: %v", spec.ID, err)
		return
	}
	cj.IPC = ipc

	// 6. IO Handling for Batch
	// In Batch mode, we don't attach. We use CaptureLogs to pipe output to the console.
	if ipc.Log2Sup[1] != -1 {
		readFd := ipc.Log2Sup[0]
		state.Wg.Add(1)
		go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.LogChan, &state.Wg)
	}

	// Register with Multiplexer so we can see its PID/Status
	state.mtx.Register(spec.ID, ipc.PtyMaster, cj.WorkloadPID)

	// Cleanup FDs
	if ipc.PtySlave != nil {
		ipc.PtySlave.Close()
	}
	lib.FreeFd(cj.IPC.KeepAlive[1])
}

func StartCreatorBatch(letter string, cmd string, state *appState, ipc *lib.IPC) (*Container, error) {
	// 1. Setup the basic spec
	spec := lib.DefaultSpec()
	spec.ID = fmt.Sprintf("bctor-%s1", letter)
	spec.IsNetRoot = true
	spec.Workload.Mode = lib.ModeBatch
	spec.Seccomp = lib.ProfileBatch
	spec.Workload.Args = []string{"sh", "-c", cmd}

	lib.LogInfo("[Batch] Container %s=Creator (Command: %s)", spec.ID, cmd)

	// 3. Launch (This creates the new namespaces)
	c, err := StartContainer(spec, state.LogChan, &state.scx, state.containers, ipc, &state.Wg)
	if err != nil {
		return nil, err
	}
	c.IPC = ipc

	// 4. Batch IO Handling
	// Unlike the shell, we don't close the pipe, we capture it.
	readFd := ipc.Log2Sup[0]
	state.Wg.Add(1)
	go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.LogChan, &state.Wg)

	// 5. Cleanup and Register
	// Note: We still close the Slave PTY because the workload already has its copy
	ipc.PtySlave.Close()
	state.mtx.Register(spec.ID, ipc.PtyMaster, c.WorkloadPID)

	// Ensure the KeepAlive for the root is handled so it doesn't collapse early
	lib.FreeFd(c.IPC.KeepAlive[1])

	return c, nil
}
