#include <stddef.h>          // for offsetof
#include <linux/seccomp.h>   // for struct seccomp_data
#include <linux/filter.h>    // for struct sock_filter, BPF_STMT, BPF_JUMP, etc.
#include <linux/audit.h>     // for AUDIT_ARCH_X86_64
#include <sys/syscall.h>     // for SYS_read, SYS_write, etc.
#include "rules.h"           // your ALLOW_SYSCALL, KILL_PROCESS macros

struct sock_filter filter[] = {
    // arch check
    BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, arch)),
    BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, AUDIT_ARCH_X86_64, 1, 0),
    KILL_PROCESS,

    // syscall number
    BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, nr)),

    ALLOW_SYSCALL(read),
    ALLOW_SYSCALL(write),
    ALLOW_SYSCALL(exit),
    ALLOW_SYSCALL(exit_group),
    ALLOW_SYSCALL(execve),
    // …

    KILL_PROCESS,
};

unsigned int filter_len = sizeof(filter) / sizeof(filter[0]);