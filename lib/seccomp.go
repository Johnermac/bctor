package lib

/*
#cgo CFLAGS: -I${SRCDIR}/seccomp
#cgo LDFLAGS: ${SRCDIR}/seccomp/libseccompfilter.a
#include "install.h"
*/
import "C"
import "fmt"

type Profile int

const (
	ProfileDebugShell Profile = iota // busybox /bin/sh
	ProfileWorkload                  // open port with nc
	ProfileHello                     // minimal hello-world
)

func ApplySeccomp(p Profile) error {
	var rc C.int

	switch p {
	case ProfileDebugShell:
		rc = C.install_debug_shell()
	case ProfileWorkload:
		rc = C.install_workload()
	case ProfileHello:
		rc = C.install_hello()
	default:
		return fmt.Errorf("unknown seccomp profile")
	}

	if rc != 0 {
		return fmt.Errorf("seccomp install failed: rc=%d", rc)
	}
	return nil
}
