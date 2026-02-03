visual map

---

--- Namespaces ---

```
UTS 
  └─  set: CLONE_NEWUTS

USER
  └─  set: CLONE_NEWUSER

MOUNT
  |   set: CLONE_NEWNS
  └─  gotta use unix.MS_REC|unix.MS_PRIVATE to isolate the new mount namespace properly

PID
  |   set: CLONE_NEWPID
  └─  it only apply to new processes, so we have to unshare+fork (or use clone()) to create the granchild

    parent
      └─ child (unshare NEWPID)
            └─ grandchild (PID 1, exec here)

IPC
  └─  set: CLONE_NEWIPC

NET
  |   set: CLONE_NEWNET
  └─  Bring up loopback for poc

CGROUP
  |   set: CLONE_NEWCGROUP
  └─  it hides information about other processes
```

---

--- Capabilities ---

```
 |  read caps from "/proc/<pid>/status" and compare with process before namespace isolation
 |  map caps that were added in NS
 └─ remove and test each cap individually to show impact
```

---

--- File System ---

```
 |  after the flag os NS MNT is set
 |  create a kind of handshake in pipe between parent and child for correct order of execution
 |  do 3 essential things: deny set groups, write uid and gid maps
 |  fork to create a grand-child
 └─ prepare and mount to finally create isolated file system (remember to fix some bugs)
```

--- CGroups ---

```
 |   we have to apply the controls in /sys/fs/cgroup (vfs) its kernel’s interface to resource control
 |   each folder that we create inside its a "group of control" (container) 
 |   and each file inside the folder is a config (metric)
 |   it follows the hierarchy, thats why we apply in the child and the limitations are in the grand-child
 |   cgroup.controllers are the "reader" that says which "powers" that kernel made available
 |   cgroup.subtree_control is the gate, its where u enable the "powers" to your children (processes)
 └─  then write the PID to cgroup.procs, and define the limitations of memory, cpu etc
 
 [!] there are a lot of more details that docker (for example), implements like cgroup.events, cgroup.freeze etc 
    Ill focus on that in another project tho
 [!] must be run with root in host cause we need to have control over /sys/fs/cgroup dir  
 [!] set the cgroup NS (AFTER u apply the cgroup configs) with the flag CLONE_NEWCGROUP, to limit the visibility of the container over the host 
```

--- Seccomp ---

in progress
```
 |  seccomp basically defines what the process is allowed to "say" to the kernel
 |  we define the filters using macros BPF, only whats necessary for each profile.
 |  we can map the syscalls by running "strace -f <bin>" 
 |  I wanna do something to map the syscalls in another project
 | Any syscalls that are not in the filter lists gets KILL_PROCESS
 |  we load PR_SET_NO_NEW_PRIVS before the filters, this doesnt allow that the processes gain new privileges (sudo). without this we couldnt apply the filter with normal user either
 |  then send an array of BPF instructions to kernel via syscall seccomp(), and the kernel validates and attaches to the current thread
 |  once applied the filter cant be removed, our exec already runs with the filters (based on the profile established)
 |  for each syscall that our exec runs, kernel pauses execution, and check the number of the syscall, if allowed it goes on, if not the process is killed (SIGSYS)
 └─ our exec now can only run syscall that are allowed to the specified profile

 [!] change SECCOMP_RET_KILL_PROCESS to SECCOMP_RET_LOG and run dmesg to filter the syscalls numbers
 [!] dmesg -w | grep --line-buffered "syscall=" | sed -u 's/.*syscall=\([0-9]\+\).*/\1/'
 [!] this will show which syscall is blocking your workload
 [!] we can get the name with "ausyscall <number>" = "apt install auditd" 
 
 [*] example:
 [*] [container] nc -lp 4444
 [*] [host] echo "Hello" | nc -v localhost 4444
  this can be blocked in many different ways:      
      -namespaces (by creating net NS)
      -capabilities (by removing CAPS that allow open ports)
      -seccomp (by blocking syscalls - if u dont allow read, you can open port, but cant connect)
      -even with cgroups (by limiting creating of processes)

```

--- todo ---

- add cfg namespaces as parameters
- fix bugs in caps and file system
- finish file system ReadOnly
- fix mount of proc and sys (have no idea how to do that, I think its a limitation of WSL)
- remove comments 
- improve output of diffs for better readability
- in "pipe handshake" check if user NS is enabled, if not, skip uid/gid mapping
- implement PTY attach to control multiples containers
- improve folders layout
- add workloads and more profiles to seccomp