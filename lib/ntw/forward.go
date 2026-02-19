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

	// itwll enter network namespace to dial	
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

	// copy for both sidess
	done := make(chan struct{}, 2)
	go func() { io.Copy(hostConn, containerConn); done <- struct{}{} }()
	go func() { io.Copy(containerConn, hostConn); done <- struct{}{} }()

	<-done
}

func ExecuteInNamespace(pid int, nsType lib.NamespaceType, fn func() error) error {
	
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// save host namespace
	hostNS, _ := netns.Get()
	defer hostNS.Close()
	 
	// usually /proc/<pid>/ns/net
	containerNS, err := netns.GetFromPid(pid)
	if err != nil {
		return err
	}
	defer containerNS.Close()
	
	if err := netns.Set(containerNS); err != nil {
		return err
	}

	// go back
	defer netns.Set(hostNS)
	
	return fn()
}

func (pf *PortForwarder) AddSession(id string, hostPort, containerPort int, pid int) error {
	stopChan := make(chan bool)

	// goroutine to not block cli
	err := PortForward(pid, hostPort, containerPort, stopChan)
	if err != nil {
		return err
	}
	
	pf.mu.Lock()
	pf.sessions[id] = append(pf.sessions[id], ForwardingSession{
		HostPort: hostPort,
		Stop:     stopChan,
	})
	pf.mu.Unlock()

	return nil
}

// Reaper
func (pf *PortForwarder) CleanupForward(id string) {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	if sessions, ok := pf.sessions[id]; ok {
		for _, s := range sessions {			
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
