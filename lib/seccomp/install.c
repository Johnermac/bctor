// builder
#include "rules.h"
#include "filter.c"   // or better: declare filter[] in a header and link

int install_shell_minimal(void) {
    return install_filter(filter, sizeof(filter) / sizeof(filter[0]));
}

int install_filter(struct sock_filter *filter, size_t count) {
    struct sock_fprog prog = {
        .len = count,
        .filter = filter,
    };

    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0))
        return -1;

    return syscall(__NR_seccomp, SECCOMP_SET_MODE_FILTER, 0, &prog);
}
