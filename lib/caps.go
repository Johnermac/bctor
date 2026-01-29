package lib

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Capability int

const (
	CAP_CHOWN Capability = iota
	CAP_DAC_OVERRIDE
	CAP_DAC_READ_SEARCH
	CAP_FOWNER
	CAP_FSETID
	CAP_KILL
	CAP_SETGID
	CAP_SETUID
	CAP_SETPCAP
	CAP_LINUX_IMMUTABLE
	CAP_NET_BIND_SERVICE
	CAP_NET_BROADCAST
	CAP_NET_ADMIN
	CAP_NET_RAW
	CAP_IPC_LOCK
	CAP_IPC_OWNER
	CAP_SYS_MODULE
	CAP_SYS_RAWIO
	CAP_SYS_CHROOT
	CAP_SYS_PTRACE
	CAP_SYS_PACCT
	CAP_SYS_ADMIN
	CAP_SYS_BOOT
	CAP_SYS_NICE
	CAP_SYS_RESOURCE
	CAP_SYS_TIME
	CAP_SYS_TTY_CONFIG
	CAP_MKNOD
	CAP_LEASE
	CAP_AUDIT_WRITE
	CAP_AUDIT_CONTROL
	CAP_SETFCAP
	CAP_MAC_OVERRIDE
	CAP_MAC_ADMIN
	CAP_SYSLOG
	CAP_WAKE_ALARM
	CAP_BLOCK_SUSPEND
	CAP_AUDIT_READ
)

type CapSet uint64

const (
	CapBounding CapSet = 1 << iota
	CapPermitted
	CapEffective
	CapInheritable
	CapAmbient
)

type CapState struct {
	PID int

	Bounding    uint64
	Permitted   uint64
	Effective   uint64
	Inheritable uint64
	Ambient     uint64
}

type CapSetView map[Capability]bool

type CapView struct {
	Bounding    CapSetView
	Permitted   CapSetView
	Effective   CapSetView
	Inheritable CapSetView
	Ambient     CapSetView
}

type CapSpec struct {
	Set   CapSet
	Cap   Capability
	Enable bool
}

type CapPlan struct {
	Specs []CapSpec
}

type CapDiff struct {
	Set      CapSet
	Cap      Capability
	Before   bool
	After    bool
}

type CapEffect struct {
	Cap Capability
	Enables []string
	Disables []string
}


func ReadCaps(pid int) (*CapState, error) {
	path := "/proc/" + strconv.Itoa(pid) + "/status"
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cs := &CapState{PID: pid}

	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "CapInh:":
			cs.Inheritable = parseCapHex(fields[1])
		case "CapPrm:":
			cs.Permitted = parseCapHex(fields[1])
		case "CapEff:":
			cs.Effective = parseCapHex(fields[1])
		case "CapBnd:":
			cs.Bounding = parseCapHex(fields[1])
		case "CapAmb:":
			cs.Ambient = parseCapHex(fields[1])
		}
	}

	return cs, nil
}


func DiffCaps(before, after *CapState) []CapDiff {
	var diffs []CapDiff

	type setDef struct {
		set  CapSet
		name string
		b    uint64
		a    uint64
	}

	sets := []setDef{
		{CapBounding, "bounding", before.Bounding, after.Bounding},
		{CapPermitted, "permitted", before.Permitted, after.Permitted},
		{CapEffective, "effective", before.Effective, after.Effective},
		{CapInheritable, "inheritable", before.Inheritable, after.Inheritable},
		{CapAmbient, "ambient", before.Ambient, after.Ambient},
	}

	for _, s := range sets {
		changed := s.b ^ s.a // XOR = bits that changed

		if changed == 0 {
			continue
		}

		for cap := 0; cap < 64; cap++ {
			mask := uint64(1) << cap
			if changed&mask == 0 {
				continue
			}

			beforeSet := (s.b & mask) != 0
			afterSet := (s.a & mask) != 0

			diffs = append(diffs, CapDiff{
				Set:    s.set,
				Cap:    Capability(cap),
				Before: beforeSet,
				After:  afterSet,
			})
		}
	}

	return diffs
}

// improving diff output

func capsFromMask(mask uint64) []Capability {
	var caps []Capability
	for c := Capability(0); c < 64; c++ {
		if mask&(1<<c) != 0 {
			caps = append(caps, c)
		}
	}
	return caps
}

func formatCaps(caps []Capability) string {
	if len(caps) == 0 {
		return "—"
	}

	var names []string
	for _, c := range caps {
		names = append(names, capName(c))
	}
	return strings.Join(names, ", ")
}

func LogCapPosture(label string, caps *CapState) {
	fmt.Printf("\n[%s] CAPABILITY POSTURE\n", label)

	fmt.Printf("  Bounding    : %s\n", formatCaps(capsFromMask(caps.Bounding)))
	fmt.Printf("  Permitted   : %s\n", formatCaps(capsFromMask(caps.Permitted)))
	fmt.Printf("  Effective   : %s\n", formatCaps(capsFromMask(caps.Effective)))	
	fmt.Printf("  Inheritable : %s\n", formatCaps(capsFromMask(caps.Inheritable)))
	fmt.Printf("  Ambient     : %s\n", formatCaps(capsFromMask(caps.Ambient)))
	
}

func LogCapDelta(diffs []CapDiff) {
	raised := map[CapSet][]Capability{}
	dropped := map[CapSet][]Capability{}

	for _, d := range diffs {
		if !d.Before && d.After {
			raised[d.Set] = append(raised[d.Set], d.Cap)
		}
		if d.Before && !d.After {
			dropped[d.Set] = append(dropped[d.Set], d.Cap)
		}
	}

	fmt.Println("\n[CAPABILITY] DELTA")
	for set, caps := range raised {
		fmt.Printf("  +%-9s %s\n", capSetName(set), formatCaps(caps))
	}
	for set, caps := range dropped {
		fmt.Printf("  -%-9s %s\n", capSetName(set), formatCaps(caps))
	}
}



func ApplyCaps(plan CapPlan) error { return nil }
//func ExplainCap(cap Capability) CapEffect {}

func LogCapChange(diff CapDiff) {
	action := "UNCHANGED"

	switch {
	case !diff.Before && diff.After:
		action = "RAISED"
	case diff.Before && !diff.After:
		action = "DROPPED"
	}

	fmt.Printf(
		"CAP_%s %-11s %-20s %v -> %v\n",
		action,
		capSetName(diff.Set),
		capName(diff.Cap),
		diff.Before,
		diff.After,
	)
}

func capSetName(set CapSet) string {
	switch set {
	case CapBounding:
		return "BOUNDING"
	case CapPermitted:
		return "PERMITTED"
	case CapEffective:
		return "EFFECTIVE"
	case CapInheritable:
		return "INHERITABLE"
	case CapAmbient:
		return "AMBIENT"
	default:
		return "UNKNOWN"
	}
}

func capName(cap Capability) string {

	var capNames = map[Capability]string{
		CAP_CHOWN:           "CAP_CHOWN",
		CAP_DAC_OVERRIDE:    "CAP_DAC_OVERRIDE",
		CAP_DAC_READ_SEARCH: "CAP_DAC_READ_SEARCH",
		CAP_FOWNER:          "CAP_FOWNER",
		CAP_FSETID:          "CAP_FSETID",
		CAP_KILL:            "CAP_KILL",
		CAP_SETGID:          "CAP_SETGID",
		CAP_SETUID:          "CAP_SETUID",
		CAP_SETPCAP:         "CAP_SETPCAP",
		CAP_LINUX_IMMUTABLE: "CAP_LINUX_IMMUTABLE",
		CAP_NET_BIND_SERVICE:"CAP_NET_BIND_SERVICE",
		CAP_NET_BROADCAST:   "CAP_NET_BROADCAST",
		CAP_NET_ADMIN:       "CAP_NET_ADMIN",
		CAP_NET_RAW:         "CAP_NET_RAW",
		CAP_IPC_LOCK:        "CAP_IPC_LOCK",
		CAP_IPC_OWNER:       "CAP_IPC_OWNER",
		CAP_SYS_MODULE:      "CAP_SYS_MODULE",
		CAP_SYS_RAWIO:       "CAP_SYS_RAWIO",
		CAP_SYS_CHROOT:      "CAP_SYS_CHROOT",
		CAP_SYS_PTRACE:      "CAP_SYS_PTRACE",
		CAP_SYS_PACCT:       "CAP_SYS_PACCT",
		CAP_SYS_ADMIN:       "CAP_SYS_ADMIN",
		CAP_SYS_BOOT:        "CAP_SYS_BOOT",
		CAP_SYS_NICE:        "CAP_SYS_NICE",
		CAP_SYS_RESOURCE:    "CAP_SYS_RESOURCE",
		CAP_SYS_TIME:        "CAP_SYS_TIME",
		CAP_SYS_TTY_CONFIG:  "CAP_SYS_TTY_CONFIG",
		CAP_MKNOD:           "CAP_MKNOD",
		CAP_LEASE:           "CAP_LEASE",
		CAP_AUDIT_WRITE:     "CAP_AUDIT_WRITE",
		CAP_AUDIT_CONTROL:   "CAP_AUDIT_CONTROL",
		CAP_SETFCAP:         "CAP_SETFCAP",
		CAP_MAC_OVERRIDE:    "CAP_MAC_OVERRIDE",
		CAP_MAC_ADMIN:       "CAP_MAC_ADMIN",
		CAP_SYSLOG:          "CAP_SYSLOG",
		CAP_WAKE_ALARM:      "CAP_WAKE_ALARM",
		CAP_BLOCK_SUSPEND:   "CAP_BLOCK_SUSPEND",
		CAP_AUDIT_READ:      "CAP_AUDIT_READ",
	}

	if name, ok := capNames[cap]; ok {
		return name
	}
	return fmt.Sprintf("CAP_%d", cap)
}



func LogCaps(label string, cs *CapState) {
	fmt.Printf("[%s] Caps PID=%d\n", label, cs.PID)
	fmt.Printf("  BND=%016x\n", cs.Bounding)
	fmt.Printf("  PRM=%016x\n", cs.Permitted)
	fmt.Printf("  EFF=%016x\n", cs.Effective)
	fmt.Printf("  INH=%016x\n", cs.Inheritable)
	fmt.Printf("  AMB=%016x\n", cs.Ambient)
}


func (c *CapState) View() CapView {
	return CapView{
		Bounding:    expandCaps(c.Bounding),
		Permitted:   expandCaps(c.Permitted),
		Effective:   expandCaps(c.Effective),
		Inheritable: expandCaps(c.Inheritable),
		Ambient:     expandCaps(c.Ambient),
	}
}


func parseCapHex(s string) uint64 {
	v, _ := strconv.ParseUint(s, 16, 64)
	return v
}

func expandCaps(mask uint64) CapSetView {
	out := make(CapSetView)

	for c := Capability(0); c <= CAP_AUDIT_READ; c++ {
		out[c] = (mask & (1 << c)) != 0
	}

	return out
}
