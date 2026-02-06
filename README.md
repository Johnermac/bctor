visual map

---

--- Namespaces ---

```go
UTS 
  |   set: CLONE_NEWUTS
  └─  it isolates hostname and NIS domain name

USER
  |   set: CLONE_NEWUSER
  └─  it allows a process to have CAP_SYS_ADMIN (root) inside the namespace while remaining an unprivileged user on the host

MOUNT
  |   set: CLONE_NEWNS
  └─  gotta use unix.MS_REC|unix.MS_PRIVATE to isolate the new mount namespace properly

IPC
  |  set: CLONE_NEWIPC
  └─ it prevents a process from accessing shared memory by the host

NET
  |   set: CLONE_NEWNET
  └─  it isolates the network stack: interfaces, routing tables, iptables
  [!] gotta manually bring up loopback interface. Ive used netlink for this

CGROUP
  |   set: CLONE_NEWCGROUP
  └─  it isolates the view of /proc/self/cgroup
  [!] This prevents the container from knowing its own limits or seeing the hosts cgroup structure

PID
  |   set: CLONE_NEWPID
  └─  it isolates process IDs
  [!] it only applies to new processes tho, so we have to fork() or clone() to create the granchild

    parent
      └─ child (unshare NEWPID)
            └─ grandchild (PID 1, exec here)
```

---

--- Capabilities ---

```go
 |   read caps from "/proc/<pid>/status" and compare with process before namespace isolation
 |   map caps that were added in NS
 └─  remove and test each cap individually to show impact
 [!] if some gains "root" inside the container, they are still limited by the bounding set of capabilities
 [!] there are a couple of capabilities sets, these sets determine which privileges are inherited, kept, or dropped during transitions like the fork we used

 The capabilities are like infinity stones of Thanos:
 [*] Permitted (CapPrm)
 [*] Effective (CapEff)
 [*] Inheritable (CapInh)
 [*] Bounding (CapBnd) 
 [*] Ambient (CapAmb)

 More details about them in my post
```

---

--- File System ---

```go
 |  after the flag of NS MNT is set
 |  create a kind of handshake in pipe between parent and child for correct order of execution
 |  do 3 essential things: deny set groups, write uid and gid maps
 |  fork to create a grand-child
 └─ prepare and mount to finally create isolated file system
 [!] we gotta bind mount of the directory onto itself, this turns it into a mount point that the kernel can accept 
 [!] here is where the "container jail" is built, all the controls are applied before the final exec

 Import distinction between chroot and pivot_root:
 [!] chroot only changes the "view" of the root. The old root is still there, and a process can "break out" (chroot escape)
 [!] it moves the entire operating systems mount tree. It swaps the current root with the new root... then we umount the .old_root with 'MNT_DETACH'. Which will impossibilitate the container to reach the host file system
 ```

--- CGroups ---

```go
 |   we have to apply the controls in /sys/fs/cgroup (vfs) its kernel’s interface to resource control
 |   each folder that we create inside its a "group of control" (container) 
 |   and each file inside the folder is a config (metric)
 |   it follows the hierarchy, thats why we apply in the child and the limitations are in the grand-child
 |   cgroup.controllers are the "reader" that says which "powers" that kernel made available
 |   cgroup.subtree_control is the gate, its where u enable the "powers" to your children (processes) -> exclusive of v2 tho, which is what Ive used
 └─  then define the limitations of memory, cpu and write the PID to cgroup.procs
 
 [!] there are a lot of more details that docker (for example), implements like cgroup.events, cgroup.freeze etc 
    Ill focus on that in another project tho
 [!] must run with root in host cause /sys/fs/cgroup belongs to root
 [!] however we are able with systemd, delegate a specific sub-hierarchy to an unprivileged user (via 'systemd-run'), then in this case we dont need to run as root anymore
 [!!] set the cgroup NS (AFTER u apply the cgroup configs) with the flag CLONE_NEWCGROUP
```

--- Seccomp ---

```go
 |  seccomp basically defines what the process is allowed to "say" to the kernel
 |  we define the filters using macros BPF, only whats necessary for each profile.
 |  we can map the syscalls by running "strace -f <bin>" 
 |  I wanna do something to map the syscalls in another project
 |  Any syscalls that are not in the filter lists gets KILL_PROCESS
 |  we load PR_SET_NO_NEW_PRIVS before the filters, this doesnt allow that the processes gain new privileges (sudo). without this we couldnt apply the filter with normal user either
 |  then send an array of BPF instructions to kernel via syscall seccomp(), and kernel verifies the BPF code at load time and attaches to the current thread
 |  once applied the filter cant be removed, our exec already runs with the filters (based on the profile established)
 |  for each syscall that our exec runs, kernel pauses execution, and check the number of the syscall, if allowed it goes on, if not the process is killed (SIGSYS) 
 |  which by default terminates the process and creates a core dump
 └─ our exec now can only run syscall that are allowed to the specified profile

 [!] change SECCOMP_RET_KILL_PROCESS to SECCOMP_RET_LOG and run dmesg to filter the syscalls numbers
 [!] dmesg -w | grep --line-buffered "syscall=" | sed -u 's/.*syscall=\([0-9]\+\).*/\1/'
 [!] this will show which syscall is blocking your workload from running
 [!] we can get the name with "ausyscall <number>" = download with "apt install auditd" 
 ```

--- So far ---
 ```go
 [*] example:
 [*] [container] nc -lp 4444
 [*] [host] echo "Hello" | nc -v localhost 4444
  this can be "blocked" in many different ways:      
      -namespaces (by creating net NS will limit connection outside of the container)
      -capabilities (by removing CAPS that allow open ports)
      -seccomp (by blocking syscalls - if u dont allow read, you can open port, but cant connect)
      -even with cgroups (by limiting creation of processes)

```

--- Container runtime ---

in progress


--- todo ---

```go
- add cfg namespaces as parameters
- fix bugs in caps and file system
- finish file system ReadOnly
- fix mount of proc and sys (have no idea how to do that, I think its a limitation of WSL)
- remove comments 
- improve output of diffs for better readability
- in "pipe handshake" check if user NS is enabled, if not, skip uid/gid mapping
- use socket in the future instead of pipe
- implement PTY attach to control multiples containers
- improve folders layout
- add workloads and more profiles to seccomp
```