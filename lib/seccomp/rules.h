#pragma once
#include <linux/seccomp.h>
#include <linux/filter.h>
#include <linux/audit.h>
#include <sys/syscall.h>

#define ALLOW_SYSCALL(name) \
    BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, (unsigned int)SYS_##name, 0, 1), \
    BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_ALLOW)

#define KILL_PROCESS \
    BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_KILL_PROCESS)

#define TRACE_SYSCALL \
    BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_TRACE)

#define LOG_SYSCALL \
    BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_LOG)

#define TRAP_PROCESS \
    BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_TRAP)