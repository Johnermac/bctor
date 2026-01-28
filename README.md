visual map

---

--- Namespaces ---

UTS 
  └─  set: CLONE_NEWUTS

USER
  └─  set: CLONE_NEWUSER

MOUNT
  |   set: CLONE_NEWNS
  └─  gotta use unix.MS_REC|unix.MS_PRIVATE to isolate the mount namespace properly

PID
  |   set: CLONE_NEWPID
  └─  it only apply to new processes, so we have to create a grandchild here

NET
  └─  in progress

```
parent
 	└─ child (unshare NEWPID)
     		└─ grandchild (PID 1, exec here)
```

---