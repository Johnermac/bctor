package ntw

import (
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/Johnermac/bctor/lib"
	"github.com/vishvananda/netns"
)

type ForwardingSession struct {
	HostPort int
	Stop     chan bool
}

type PortForwarder struct {
	mu       sync.Mutex
	sessions map[string][]ForwardingSession // pod -> sessions
}

func NewPortForwarder() *PortForwarder {
	return &PortForwarder{
		sessions: make(map[string][]ForwardingSession),
	}
}

func PortForward(targetPID int, hostPort, containerPort int, stop chan bool) error {
	addr := fmt.Sprintf("0.0.0.0:%d", hostPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", addr, err)
	}

	//fmt.Printf(" [DEBUG] PortForward: Listener started on %s\n", addr)

	// cleanup signal closes the listener and unblocks Accept.
	go func() {
		<-stop
		_ = l.Close()
	}()

	go func() {
		defer l.Close()
		for {
			conn, err := l.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				fmt.Printf(" [DEBUG] PortForward: Accept error: %v\n", err)
				return
			}
			//fmt.Printf(" [DEBUG] PortForward: New connection from %s\n", conn.RemoteAddr())
			go handleForwardConn(targetPID, conn, containerPort)
		}
	}()
	return nil
}

func handleForwardConn(pid int, hostConn net.Conn, containerPort int) {
	defer hostConn.Close()

	// Enter the container's network namespace to dial
	// This is the "magic" part. We use a closure or a separate thread.
	var containerConn net.Conn
	err := ExecuteInNamespace(pid, lib.NSNet, func() error {
		var dialErr error
		containerConn, dialErr = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", containerPort), 2*time.Second)
		return dialErr
	})

	if err != nil {
		return
	}
	defer containerConn.Close()

	// 3. Splice the data (Bi-directional copy)
	done := make(chan struct{}, 2)
	go func() { io.Copy(hostConn, containerConn); done <- struct{}{} }()
	go func() { io.Copy(containerConn, hostConn); done <- struct{}{} }()

	<-done
}

func ExecuteInNamespace(pid int, nsType lib.NamespaceType, fn func() error) error {
	// 1. Lock the OS thread so Go doesn't move us to a different thread mid-execution
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 2. Save the current (Host) namespace so we can go back later
	hostNS, _ := netns.Get()
	defer hostNS.Close()

	// 3. Get a handle to the Container's namespace
	// This is the "NamespaceNet" part (usually /proc/<pid>/ns/net)
	containerNS, err := netns.GetFromPid(pid)
	if err != nil {
		return err
	}
	defer containerNS.Close()

	// 4. Switch to the container's namespace
	if err := netns.Set(containerNS); err != nil {
		return err
	}

	// 5. Switch back to host namespace when the function finishes
	defer netns.Set(hostNS)

	// 6. Run the actual Dialing logic
	return fn()
}

func (pf *PortForwarder) AddSession(id string, hostPort, containerPort int, pid int) error {
	stopChan := make(chan bool)

	// 1. Start the actual networking logic (The code we wrote earlier)
	// We run it in a goroutine so it doesn't block the CLI
	err := PortForward(pid, hostPort, containerPort, stopChan)
	if err != nil {
		return err
	}

	// 2. Save the session so we can kill it later
	pf.mu.Lock()
	pf.sessions[id] = append(pf.sessions[id], ForwardingSession{
		HostPort: hostPort,
		Stop:     stopChan,
	})
	pf.mu.Unlock()

	return nil
}

// CleanupContainer is called by the Reaper
func (pf *PortForwarder) CleanupForward(id string) {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	if sessions, ok := pf.sessions[id]; ok {
		for _, s := range sessions {
			// Signal the listener to close
			select {
			case s.Stop <- true:
			default:
			}
		}
		delete(pf.sessions, id)
	}
}

func (pf *PortForwarder) List(id string) []int {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	var ports []int
	for _, s := range pf.sessions[id] {
		ports = append(ports, s.HostPort)
	}
	return ports
}
