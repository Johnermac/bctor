#pragma once
#include <linux/filter.h>
#include <stddef.h>

int install_filter(struct sock_filter *filter, size_t count);
int install_shell_minimal(void);
