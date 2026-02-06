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

func ApplyNamespaces(cfg NamespaceConfig) error {
	var flags int

	if cfg.USER {
		//flags |= unix.CLONE_NEWUSER
		if err := unix.Unshare(unix.CLONE_NEWUSER); err != nil {
			return err
		}
	}

	if cfg.UTS {
		flags |= unix.CLONE_NEWUTS
	}
	if cfg.MOUNT {
		flags |= unix.CLONE_NEWNS
	}
	if cfg.PID {
		flags |= unix.CLONE_NEWPID
	}
	if cfg.NET {
		flags |= unix.CLONE_NEWNET
	}
	if cfg.IPC {
		flags |= unix.CLONE_NEWIPC
	}

	if flags == 0 {
		return nil
	}

	err := unix.Unshare(flags)
	if err != nil {
		return fmt.Errorf("unshare falhou: %v", err)
	}

	err = flagsChecks(cfg)
	if err != nil {
		return fmt.Errorf("flag check falhou: %v", err)
	}

	return nil
}

func flagsChecks(cfg NamespaceConfig) error {
	if cfg.MOUNT {
		// This is equivalent to 'mount --make-rprivate /'
		// It ensures child mounts don't leak to the parent system
		err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")
		if err != nil {
			return err
		}
	}

	if cfg.NET {

		// Bring up loopback using netlink
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return fmt.Errorf("cannot find lo: %w", err)
		}
		if err := netlink.LinkSetUp(lo); err != nil {
			return fmt.Errorf("cannot bring up lo: %w", err)
		}

		// interfaces
		links, _ := netlink.LinkList()
		for _, l := range links {
			fmt.Printf("[*] %v: %v\n", l.Attrs().Name, l.Attrs().Flags)
		}
	}

	if cfg.IPC {
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
