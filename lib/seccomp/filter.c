#include <stddef.h>          // for offsetof
#include <linux/seccomp.h>   // for struct seccomp_data
#include <linux/filter.h>    // for struct sock_filter, BPF_STMT, BPF_JUMP, etc.
#include <linux/audit.h>     // for AUDIT_ARCH_X86_64
#include <sys/syscall.h>     // for SYS_read, SYS_write, etc.
#include "rules.h"           // your ALLOW_SYSCALL, KILL_PROCESS macros


#define VALIDATE_ARCH \
    BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, arch)), \
    BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, AUDIT_ARCH_X86_64, 1, 0), \
    KILL_PROCESS, \
    BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, nr))


// --- Perfil Minimal: For static binaries or very simple tasks ---
struct sock_filter filter_hello[] = {
    VALIDATE_ARCH,
    ALLOW_SYSCALL(write),        // Write data to stdout/stderr
    ALLOW_SYSCALL(exit),         // Terminate current thread
    ALLOW_SYSCALL(exit_group),   // Terminate process and all threads
    ALLOW_SYSCALL(rt_sigreturn), // Return from signal handler
    KILL_PROCESS
};
    
// --- Perfil Workload: Optimized for Netcat (nc) and Go Runtime ---
struct sock_filter filter_workload[] = {
    VALIDATE_ARCH,
    /* Initialization & Memory */
    ALLOW_SYSCALL(execve),       // Execute the binary
    ALLOW_SYSCALL(brk),          // Extend heap memory
    ALLOW_SYSCALL(mmap),         // Map memory pages (allocation/libs)
    ALLOW_SYSCALL(munmap),       // Unmap memory pages
    ALLOW_SYSCALL(mprotect),     // Change memory protection (NX/RO)
    ALLOW_SYSCALL(arch_prctl),   // Set architecture-specific thread state (TLS)
    ALLOW_SYSCALL(prlimit64),    // Get/set process resource limits

    /* Runtime & Identity */
    ALLOW_SYSCALL(write),        // Standard output for logs/data
    ALLOW_SYSCALL(read),         // Read data from input/files
    ALLOW_SYSCALL(close),        // Close file descriptors
    ALLOW_SYSCALL(getuid),       // Check current user identity
    ALLOW_SYSCALL(uname),        // Get system/kernel information
    ALLOW_SYSCALL(getrandom),    // Get entropy for internal security/salts
    ALLOW_SYSCALL(prctl),        // Process operations (like setting name)
    ALLOW_SYSCALL(readlink),     // Resolve symbolic link paths
    ALLOW_SYSCALL(fcntl),        // File descriptor manipulation

    /* Networking */
    ALLOW_SYSCALL(socket),       // Create network endpoint
    ALLOW_SYSCALL(bind),         // Bind socket to a specific port
    ALLOW_SYSCALL(listen),       // Set socket to listen for connections
    ALLOW_SYSCALL(accept),       // Accept incoming connections (standard)
    ALLOW_SYSCALL(accept4),      // Accept incoming connections (with flags)
    ALLOW_SYSCALL(setsockopt),   // Set socket options (like REUSEADDR)
    ALLOW_SYSCALL(poll),         // Wait for events on file descriptors
    ALLOW_SYSCALL(ppoll),        // Modern version of poll with signal mask

    /* Signals & Lifecycle */
    ALLOW_SYSCALL(set_tid_address), // Thread-local storage setup
    ALLOW_SYSCALL(set_robust_list), // Manage robust futexes for crashes
    ALLOW_SYSCALL(rseq),            // Restartable sequences for speed
    ALLOW_SYSCALL(rt_sigreturn),    // Essential for signal handling
    ALLOW_SYSCALL(exit_group),      // Clean exit for all threads
    KILL_PROCESS
};

// --- Perfil Debug: Wide allowlist for full shell/busybox capability ---
struct sock_filter filter_debug[] = {    
    VALIDATE_ARCH,
    /* File System & Navigation */
    ALLOW_SYSCALL(openat),       // Open files relative to directory
    ALLOW_SYSCALL(read),         // Read from files
    ALLOW_SYSCALL(write),        // Write to files
    ALLOW_SYSCALL(close),        // Close descriptors
    ALLOW_SYSCALL(access),       // Check file permissions
    ALLOW_SYSCALL(stat),         // Get file status
    ALLOW_SYSCALL(fstat),        // Get file status via descriptor
    ALLOW_SYSCALL(lstat),        // Get symlink status
    ALLOW_SYSCALL(newfstatat),   // Modern fstat variant
    ALLOW_SYSCALL(getdents),     // List directory entries (legacy)
    ALLOW_SYSCALL(getdents64),   // List directory entries (modern/ls)
    ALLOW_SYSCALL(readlink),     // Read symlink target
    ALLOW_SYSCALL(readlinkat),   // Read symlink target relative
    ALLOW_SYSCALL(lseek),        // Move file pointer (ps/config)
    ALLOW_SYSCALL(getcwd),       // Get current working directory (pwd)
    ALLOW_SYSCALL(chdir),        // Change directory (cd)
    ALLOW_SYSCALL(pipe),         // Create inter-process pipe
    ALLOW_SYSCALL(pipe2),        // Create pipe with flags

    /* Process Management */
    ALLOW_SYSCALL(execve),       // Execute new programs
    ALLOW_SYSCALL(clone),        // Create child processes (fork)
    ALLOW_SYSCALL(wait4),        // Wait for process termination
    ALLOW_SYSCALL(exit_group),   // Terminate process group
    ALLOW_SYSCALL(getpid),       // Get process ID
    ALLOW_SYSCALL(getppid),      // Get parent process ID
    ALLOW_SYSCALL(getpgrp),      // Get process group ID
    ALLOW_SYSCALL(setpgid),      // Set process group ID (job control)

    /* Memory & System */
    ALLOW_SYSCALL(brk),          // Heap management
    ALLOW_SYSCALL(mmap),         // Memory mapping
    ALLOW_SYSCALL(munmap),       // Memory unmapping
    ALLOW_SYSCALL(mprotect),     // Set memory protection
    ALLOW_SYSCALL(pread64),      // Read from offset (binary loading)
    ALLOW_SYSCALL(arch_prctl),   // Arch-specific process control
    ALLOW_SYSCALL(prlimit64),    // Manage resource limits
    ALLOW_SYSCALL(uname),        // Get system info
    ALLOW_SYSCALL(getrandom),    // Entropy
    ALLOW_SYSCALL(prctl),        // Process-wide control (name/seccomp)
    ALLOW_SYSCALL(futex),        // Fast userspace locking (mutexes)

    /* Identity & Caps */
    ALLOW_SYSCALL(getuid),       // Get real user ID
    ALLOW_SYSCALL(geteuid),      // Get effective user ID
    ALLOW_SYSCALL(getgid),       // Get real group ID
    ALLOW_SYSCALL(getegid),      // Get effective group ID
    ALLOW_SYSCALL(getresuid),    // Get real, effective, saved UIDs
    ALLOW_SYSCALL(getresgid),    // Get real, effective, saved GIDs
    ALLOW_SYSCALL(getgroups),    // Get supplementary groups
    ALLOW_SYSCALL(setuid),       // Set user identity
    ALLOW_SYSCALL(setgid),       // Set group identity
    ALLOW_SYSCALL(setgroups),    // Set supplementary groups
    ALLOW_SYSCALL(capget),       // Get process capabilities
    ALLOW_SYSCALL(capset),       // Set process capabilities

    /* Signals & IO */
    ALLOW_SYSCALL(rt_sigaction), // Set signal handler
    ALLOW_SYSCALL(rt_sigprocmask), // Block/unblock signals
    ALLOW_SYSCALL(rt_sigreturn), // Signal handler return
    ALLOW_SYSCALL(sigaltstack),  // Define alternate signal stack
    ALLOW_SYSCALL(poll),         // Wait for file events
    ALLOW_SYSCALL(select),       // Wait for file events (old)
    ALLOW_SYSCALL(ioctl),        // Terminal & Device control
    ALLOW_SYSCALL(fcntl),        // File descriptor control
    ALLOW_SYSCALL(dup),          // Duplicate file descriptor
    ALLOW_SYSCALL(dup2),         // Duplicate to specific fd
    ALLOW_SYSCALL(dup3),         // Duplicate with flags

    /* Networking (Support for ID/Network utilities) */
    ALLOW_SYSCALL(socket),       // Network endpoint creation
    ALLOW_SYSCALL(connect),      // Initiate network connection
    
    /* Glibc Helpers */
    ALLOW_SYSCALL(set_tid_address), // Thread-ID pointer
    ALLOW_SYSCALL(set_robust_list), // Robust futex list
    ALLOW_SYSCALL(rseq),            // Restartable sequences

    KILL_PROCESS
};


unsigned short len_hello = sizeof(filter_hello) / sizeof(filter_hello[0]);
unsigned short len_workload = sizeof(filter_workload) / sizeof(filter_workload[0]);
unsigned short len_debug = sizeof(filter_debug) / sizeof(filter_debug[0]);