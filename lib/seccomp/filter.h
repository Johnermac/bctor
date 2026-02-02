#pragma once
#include <linux/filter.h>

extern struct sock_filter filter_hello[];
extern unsigned short len_hello;

extern struct sock_filter filter_init[];
extern unsigned short len_init;

extern struct sock_filter filter_debug[];
extern unsigned short len_debug;
