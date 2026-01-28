package lib

import (
	"golang.org/x/sys/unix"
)

type NamespaceConfig struct {
	UTS   bool
	MOUNT bool
	PID   bool
	NET   bool
	USER  bool
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

	if flags == 0 {
		return nil
	}

	err := unix.Unshare(flags)
	if err != nil {
		return err
	}

	err = flagsChecks(cfg)
	if err != nil {
		return err
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
	return nil
}
