note for later on how I can grab the fork and get the response from that

```go
pidForCap, err := lib.NewFork()
		if err != nil {
				os.Stdout.WriteString("Error in NewFork CHILD: " + err.Error() + "\n")
		}

		if pidForCap == 0 {
				// Child branch
				myPid := os.Getpid()
				capStateChild, err := lib.ReadCaps(myPid)
				if err != nil {
						os.Stdout.WriteString("Error in ReadCaps for CHILD-after-cap-drop: " + err.Error() + "\n")
				}
				lib.LogCapPosture("grand-child (post-cap-ambient)", capStateChild)					
				os.Exit(0)
		} else {
				// Parent branch
				// You can also inspect the child externally:
				//capStateChild, err := lib.ReadCaps(int(pidForCap))
				//if err != nil {
				//		os.Stdout.WriteString("Error in ReadCaps for CHILD-after-cap-drop: " + err.Error() + "\n")
				//}
				//lib.LogCaps("CHILD", capStateChild)
		}
```


[note] how to call ExplainCap 

```go
for _, d := range diffs {
	if d.To {
		e := ExplainCap(d.Cap)
		LogCapEffect(e) //finish this lkater
	}
}

```