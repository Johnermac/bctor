#pragma once
#include <linux/filter.h>
#include <stddef.h>

int install_filter(struct sock_filter *filter, size_t count);
int install_debug_shell(void); // wide allowlist (~70 syscalls)
int install_init(void);        // mount/pivot/cgroup only
int install_hello(void);       // write, exit, sigreturn
