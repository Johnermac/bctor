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
```

---

--- Capabilities ---

in progress...

```
read caps from "/proc/<pid>/status" and compare with process before namespace isolation
map caps that were added in NS
remove and test each cap individually to show impact
```
