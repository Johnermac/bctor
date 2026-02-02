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
    ProfileShellMinimal Profile = iota
)

func ApplySeccomp(p Profile) error {
    switch p {
    case ProfileShellMinimal:
        rc := C.install_shell_minimal()
        if rc != 0 {
            return fmt.Errorf("seccomp install failed: return code %d", rc)
        }
    default:
        return fmt.Errorf("unknown seccomp profile")
    }
    return nil
}
