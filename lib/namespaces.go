package lib

import (
	"fmt"
	"os"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type NamespaceConfig struct {
	USER 		bool
	NET 		bool
	MOUNT 	bool
	PID   	bool
	IPC   	bool
	UTS   	bool
	CGROUP 	bool
}
	

func ApplyNamespaces(spec *ContainerSpec, ipc *IPC) error {
	var flags int
	shared := make(map[NamespaceType]int)

	// 0. Receive shared namespace FDs (if any)
	if len(spec.Shares) > 0 {
		shared = RecvNamespaceFDs(ipc)
	}

	// 1. USER namespace FIRST (special case)
	_, joiningUser := shared[NSUser]

	if spec.Namespaces.USER && !joiningUser {
		fmt.Printf("--[>] Init: Creating new userns\n")
		if err := unix.Unshare(unix.CLONE_NEWUSER); err != nil {
			return fmt.Errorf("unshare(user): %w", err)
		}

		// Signal supervisor: userns is ready
		unix.Write(ipc.UserNSReady[1], []byte{1})
		unix.Close(ipc.UserNSReady[1])

		// Wait for uid/gid maps
		fmt.Fprintf(os.Stderr, "--[>] Init: Handshake\n")
		if err := WaitForUserNSSetup(ipc); err != nil {
			return fmt.Errorf("--[?] wait user ns setup failed: %w", err)
		}
		unix.Close(ipc.UserNSPipe[0])
		unix.Close(ipc.UserNSPipe[1])

	} else if joiningUser {
		fmt.Printf("--[>] Init: Joining shared userns=%d\n", shared[NSUser])
		if err := unix.Setns(shared[NSUser], unix.CLONE_NEWUSER); err != nil {		
			return fmt.Errorf("setns(user): %w", err)
		}
		unix.Close(shared[NSUser])
	}

	// 2. Join shared namespaces (after USER exists)
	for ns, fd := range shared {
		if ns == NSUser {
			continue // already handled
		}
		if err := unix.Setns(fd, nsTypeToCloneFlag(ns)); err != nil {
			return fmt.Errorf("setns(%v): %w", ns, err)
		}
		unix.Close(fd)
	}

	// 3. Build unshare flags for non-shared namespaces
	for _, ns := range []NamespaceType{
		NSNet, NSMnt, NSPID, NSIPC, NSUTS, NSCgroup,
	} {		
		if specWantsNamespace(spec, ns) {
			if _, joined := shared[ns]; !joined {
				flags |= nsTypeToCloneFlag(ns)
			}
		}
	}

	// 4. Unshare remaining namespaces
	if flags != 0 {
		fmt.Printf("--[>] Init: Unsharing namespaces with flags: %x\n", flags)
		if err := unix.Unshare(flags); err != nil {
			return fmt.Errorf("unshare: %w", err)
		}
	}

	if err := flagsChecks(spec); err != nil {
		return fmt.Errorf("--[?] flag check failed: %v", err)
	}

	return nil
}


func flagsChecks(spec *ContainerSpec) error {
	if spec.Namespaces.MOUNT {
		// This is equivalent to 'mount --make-rprivate /'
		// It ensures child mounts don't leak to the parent system
		err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")
		if err != nil {
			return err
		}
	}

	if spec.Namespaces.NET {

		// Bring up loopback using netlink
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return fmt.Errorf("--[?] Init: cannot find lo: %w", err)
		}
		if err := netlink.LinkSetUp(lo); err != nil {
			return fmt.Errorf("--[?] Init: cannot bring up lo: %w", err)
		}
		// interfaces
		links, _ := netlink.LinkList()
		for _, l := range links {
			fmt.Printf("--[*] Init: %v: %v\n", l.Attrs().Name, l.Attrs().Flags)
		}

	}

	if spec.Namespaces.IPC {
		// todo later some implmentation
		// for now the ns-ipc works for poc

	}

	return nil
}

func (c NamespaceConfig) AnyEnabled() bool {
	return c.USER || c.MOUNT || c.CGROUP || c.PID || c.UTS || c.NET || c.IPC
}

func LogNamespace(parentNS *NamespaceState, pid int) {
	childNS, _ := ReadNamespaces(pid)
	nsdiff := DiffNamespaces(parentNS, childNS)
	LogNamespaceDelta(nsdiff)
}

func nsTypeToCloneFlag(t NamespaceType) int {
	switch t {
	case NSUser:
		return unix.CLONE_NEWUSER
	case NSNet:
		return unix.CLONE_NEWNET
	case NSMnt:
		return unix.CLONE_NEWNS
	case NSPID:
		return unix.CLONE_NEWPID
	case NSIPC:
		return unix.CLONE_NEWIPC
	case NSUTS:
		return unix.CLONE_NEWUTS
	case NSCgroup:
		return unix.CLONE_NEWCGROUP
	default:
		panic("unknown ns type")
	}
}

func specWantsNamespace(spec *ContainerSpec, ns NamespaceType) bool {
	switch ns {
	case NSUser:
		return spec.Namespaces.USER
	case NSNet:
		return spec.Namespaces.NET
	case NSMnt:
		return spec.Namespaces.MOUNT
	case NSPID:
		return spec.Namespaces.PID
	case NSIPC:
		return spec.Namespaces.IPC
	case NSUTS:
		return spec.Namespaces.UTS
	case NSCgroup:
		return spec.Namespaces.CGROUP
	default:
		return false
	}
}

func nsTypeToProcPath(ns NamespaceType) string {
	switch ns {
	case NSUser:
		return "/proc/self/ns/user"
	case NSNet:
		return "/proc/self/ns/net"
	case NSMnt:
		return "/proc/self/ns/mnt"
	case NSPID:
		return "/proc/self/ns/pid"
	case NSIPC:
		return "/proc/self/ns/ipc"
	case NSUTS:
		return "/proc/self/ns/uts"
	case NSCgroup:
		return "/proc/self/ns/cgroup"
	default:
		panic("unknown namespace type")
	}
}
