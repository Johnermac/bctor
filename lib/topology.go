package lib

import (
	"golang.org/x/sys/unix"
)

type NamespaceConfig struct {
	UTS   bool
	Mount bool
	PID   bool
	Net   bool
	User  bool
}

func ApplyNamespaces(cfg NamespaceConfig) error {
	var flags int

	if cfg.UTS {
		flags |= unix.CLONE_NEWUTS
	}
	if cfg.Mount {
		flags |= unix.CLONE_NEWNS
	}
	if cfg.PID {
		flags |= unix.CLONE_NEWPID
	}
	if cfg.Net {
		flags |= unix.CLONE_NEWNET
	}
	if cfg.User {
		flags |= unix.CLONE_NEWUSER
	}

	if flags == 0 {
		return nil
	}

	return unix.Unshare(flags)
}
