

## [note] how to call ExplainCap 

```go
for _, d := range diffs {
	if d.To {
		e := ExplainCap(d.Cap)
		LogCapEffect(e) //finish this lkater
	}
}

```


## double pipe setup

create pipe2
```go
var p2c [2]int
	err := unix.Pipe2(p2c[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

var c2p [2]int
	err = unix.Pipe2(c2p[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
}
```

child

```go
unix.Close(p2c[1]) 
unix.Close(c2p[0])

os.Stdout.WriteString("1\n")
unix.Write(c2p[1], []byte("G")) 
    
buf := make([]byte, 1)
unix.Read(p2c[0], buf) 
    
os.Stdout.WriteString("4\n") 
```

parent
```go
unix.Close(p2c[0]) // Pai só escreve no p2c
unix.Close(c2p[1]) // Pai só lê do c2p

// 1. Espera o Filho avisar que nasceu
buf := make([]byte, 1)
unix.Read(c2p[0], buf)

os.Stdout.WriteString("2\n")

pidStr := strconv.Itoa(int(pid)) //child pid


if err := lib.SetupUserNamespace(pidStr); err != nil {
	os.Stdout.WriteString("SetupUserNamespace failed: " + err.Error() + "\n")
	unix.Exit(1)
}

os.Stdout.WriteString("3\n")
unix.Write(p2c[1], []byte("K"))
```


freeze setup
it should be applied by the parent of the process. 
it "disables" the cgroup limitations applied
gonna use this for debug only

```go
// freeze
if err := SetCgroupFreeze(Rootfs, true); err != nil {
	log.Fatal(err)
}

// unfreeze
if err := SetCgroupFreeze(Rootfs, false); err != nil {
	log.Fatal(err)
}
```

log caps
```go
capStateChild, _ := ReadCaps(os.Getpid())
if err != nil {
	os.Stdout.WriteString("Error in ReadCaps: " + err.Error() + "\n")
}
LogCapPosture("grand-child", capStateChild)
```