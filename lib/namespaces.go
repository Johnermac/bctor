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

func ApplyNamespaces(spec *ContainerSpec, ipc *IPC) error {

	var flags int
	var userfd, netfd int

	sharingUser := spec.ShareUserNS != nil
	sharingNet  := spec.ShareNetNS != nil
	creatingUser := spec.Namespaces.USER && !sharingUser	

	if sharingUser || sharingNet {
		fmt.Printf("--[>] Init: RecvUserNetNSFD\n")
		userfd, netfd = RecvUserNetNSFD(ipc)
		fmt.Printf("--[>] Init: UserFD=%d and NetFD=%d\n", userfd, netfd)
		unix.Close(ipc.Sup2Init[0])
	} 	

	// ---- USER namespace ----
	if sharingUser {
		fmt.Printf("--[>] Init: Joining shared userns fd=%d\n", userfd)
		spec.ShareUserNS.FD = userfd
		if err := unix.Setns(spec.ShareUserNS.FD, unix.CLONE_NEWUSER); err != nil {
			return fmt.Errorf("setns(user): %w", err)
		}
		unix.Close(userfd)
	} else if creatingUser {
		fmt.Printf("--[>] Init: Creating new userns\n")
		if err := unix.Unshare(unix.CLONE_NEWUSER); err != nil {
			return fmt.Errorf("unshare(user): %w", err)
		}

		// Wait for uid/gid maps
		fmt.Printf("--[>] Init: Handshake\n")
		if err := WaitForUserNSSetup(ipc.UserNSPipe[0]); err != nil {
			return fmt.Errorf("--[?] wait user ns setup failed: %w", err)
		}
		unix.Close(ipc.UserNSPipe[0])
		unix.Close(ipc.UserNSPipe[1])
	}

	// ---- NET namespace ----
	if sharingNet {
		fmt.Printf("--[>] Init: Joining shared netns fd=%d\n", netfd)
		spec.ShareNetNS.FD = netfd
		if err := joinNetNS(spec.ShareNetNS.FD); err != nil {
			return fmt.Errorf("setns(net): %w", err)
		}
		unix.Close(netfd)
	} else if spec.Namespaces.NET {
		fmt.Printf("--[>] Init: Creating new netns\n")
		flags |= unix.CLONE_NEWNET
	}


	if spec.Namespaces.UTS {
			flags |= unix.CLONE_NEWUTS
	}
	if spec.Namespaces.MOUNT {
			flags |= unix.CLONE_NEWNS
	}
	if spec.Namespaces.PID {
			flags |= unix.CLONE_NEWPID
	}
	if spec.Namespaces.IPC {
			flags |= unix.CLONE_NEWIPC
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
