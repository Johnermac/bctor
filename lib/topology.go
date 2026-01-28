package lib

import (
	"fmt"
	"os"
	"strconv"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type PIDRole int

const (
	PIDRoleContinue PIDRole = iota // normal path
	PIDRoleInit                    // PID 1 in new namespace
	PIDRoleExit                    // intermediate child
)

type NamespaceConfig struct {
	UTS   bool
	MOUNT bool
	PID   bool
	NET   bool
	USER  bool
	IPC   bool
}

func ApplyNamespaces(cfg NamespaceConfig) error {
	var flags int

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
	if cfg.USER {
		flags |= unix.CLONE_NEWUSER
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
			fmt.Printf("%v: %v\n", l.Attrs().Name, l.Attrs().Flags)
		}
	}

	if cfg.IPC {
		// todo later some implmentation
		// for now the ns-ipc works for poc

	}

	return nil
}

func ResolvePIDNamespace(enabled bool, writeFD int) (PIDRole, error) {
	if !enabled {
		return PIDRoleContinue, nil
	}

	pid, _, errno := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
	if errno != 0 {
		return PIDRoleContinue, errno
	}

	if pid == 0 {
		// NETO: Envia o PID real para o Pai e retorna para fazer o Exec
		myHostPID := strconv.Itoa(os.Getpid())
		unix.Write(writeFD, []byte(myHostPID+"\n"))
		unix.Close(writeFD)
		return PIDRoleInit, nil
	}

	// INTERMEDIÁRIO: Sai imediatamente
	return PIDRoleExit, nil

	/*
			*** mental map for PID namespace implmentation ***

			parent
		 	└─ child (unshare NEWPID)
		     		└─ grandchild (PID 1, exec here)
	*/
}
