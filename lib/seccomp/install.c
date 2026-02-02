#include <stddef.h>        // for size_t
#include <unistd.h>        // for syscall()
#include <sys/prctl.h>     // for prctl(), PR_SET_NO_NEW_PRIVS
#include <linux/seccomp.h> // for SECCOMP_SET_MODE_FILTER
#include <linux/filter.h>  // for struct sock_filter, struct sock_fprog
#include "install.h"       // your own header with prototypes
#include "filter.h"        // declare extern filter[] here
#include <sys/syscall.h> 

#ifndef __NR_seccomp
#define __NR_seccomp 317
#endif

int install_shell_minimal(void) {
    return install_filter(filter, (unsigned short)filter_len);
}

int install_filter(struct sock_filter *filter, size_t count) {
    struct sock_fprog prog = {
        .len = (unsigned short)count,
        .filter = filter,
    };

    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0))
        return -1;

    return syscall(__NR_seccomp, SECCOMP_SET_MODE_FILTER, 0, &prog);
}
