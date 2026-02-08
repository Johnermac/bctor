package lib

import (
	"fmt"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type PIDRole int

const (
	PIDRoleContinue PIDRole = iota // normal path
	PIDRoleInit                    // PID 1 in new namespace
	PIDRoleExit                    // child
)

type NamespaceConfig struct {
	UTS    bool
	MOUNT  bool
	PID    bool
	NET    bool
	USER   bool
	IPC    bool
	CGROUP bool
}

func ApplyNamespaces(spec *ContainerSpec) error {
    var flags int

    // USER namespace handled separately
    if spec.Namespaces.USER {
        if err := unix.Unshare(unix.CLONE_NEWUSER); err != nil {
            return err
        }
    }

    switch {
    case spec.Namespaces.UTS:
        flags |= unix.CLONE_NEWUTS
    case spec.Namespaces.MOUNT:
        flags |= unix.CLONE_NEWNS
    case spec.Namespaces.PID:
        flags |= unix.CLONE_NEWPID
		case spec.Namespaces.IPC:
        flags |= unix.CLONE_NEWIPC
    }

    switch {
		case spec.ShareNetNS != nil:
			fmt.Printf("--[>] Init: Joining shared netns fd %d\n", spec.ShareNetNS.FD)
			if err := joinNetNS(spec.ShareNetNS.FD); err != nil {
				return err
			}

		case spec.Namespaces.NET:
			fmt.Println("--[>] Init: Creating new netns")
			flags |= unix.CLONE_NEWNET
		}
   

    if flags == 0 { return nil }

    if err := unix.Unshare(flags); err != nil {
        return fmt.Errorf("--[?] unshare failed: %v", err)
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

func NewFork() (uintptr, error) {
	pid, _, err := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
	if err != 0 {
		return 0, err
	}
	return pid, nil
}

func (c NamespaceConfig) AnyEnabled() bool {
	return c.USER || c.MOUNT || c.CGROUP || c.PID || c.UTS || c.NET || c.IPC
}

func LogNamespace(parentNS *NamespaceState, pid int) {
	childNS, _ := ReadNamespaces(pid)
	nsdiff := DiffNamespaces(parentNS, childNS)
	LogNamespaceDelta(nsdiff)
}
