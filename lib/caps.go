package lib

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
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
	Set    CapSet
	Cap    Capability
	Enable bool
}

type CapPlan struct {
	Specs []CapSpec
}

type CapDiff struct {
	Set    CapSet
	Cap    Capability
	Before bool
	After  bool
}

type CapEffect struct {
	Cap      Capability
	Enables  []string
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
func DropCapability(cap Capability) error {
	// 1. Bounding set
	if err := unix.Prctl(
		unix.PR_CAPBSET_DROP,
		uintptr(cap),
		0, 0, 0,
	); err != nil {
		return fmt.Errorf("prctl(PR_CAPBSET_DROP): %w", err)
	}

	// 2. Effective + Permitted
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0,
	}

	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capget: %w", err)
	}

	idx := cap / 32
	bit := uint(cap % 32)

	data[idx].Effective &^= (1 << bit)
	data[idx].Permitted &^= (1 << bit)

	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capset: %w", err)
	}

	return nil
}

// ambient

func ClearAmbient() error {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_CAP_AMBIENT_CLEAR_ALL): %w", err)
	}
	return nil
}

func DropAllCapabilities() error {
	// clearn Bounding

	for c := 0; c <= int(unix.CAP_LAST_CAP); c++ {
		_ = unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(c), 0, 0, 0)
	}

	// clearn (Effective, Permitted, Inheritable)
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0,
	}

	// 64 bits
	data := [2]unix.CapUserData{
		{Effective: 0, Permitted: 0, Inheritable: 0},
		{Effective: 0, Permitted: 0, Inheritable: 0},
	}

	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capset falhou ao zerar tudo: %w", err)
	}

	return nil
}

func DropAllExcept(keep Capability) {
	for c := 0; c <= int(unix.CAP_LAST_CAP); c++ {
		if Capability(c) == keep {
			continue
		}
		_ = unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(c), 0, 0, 0)
	}
}

func SetCapabilities(caps ...Capability) error {
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0,
	}

	// 1. Primeiro, lemos o estado atual para não sobrescrever outras caps por erro
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capget: %w", err)
	}

	// 2. Zeramos os conjuntos para garantir que APENAS as que passamos fiquem ativas
	data[0].Effective = 0
	data[0].Permitted = 0
	data[1].Effective = 0
	data[1].Permitted = 0

	// 3. Ativamos os bits das capacidades desejadas
	for _, c := range caps {
		idx := c / 32
		bit := uint(c % 32)

		data[idx].Effective |= (1 << bit)
		data[idx].Permitted |= (1 << bit)
	}

	// 4. Aplicamos ao processo
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capset: %w", err)
	}

	return nil
}

func RaiseAmbient(cap Capability) error {
	if err := unix.Prctl(
		unix.PR_CAP_AMBIENT,
		unix.PR_CAP_AMBIENT_RAISE,
		uintptr(cap),
		0,
		0,
	); err != nil {
		return fmt.Errorf("prctl(PR_CAP_AMBIENT_RAISE %d): %w", cap, err)
	}
	return nil
}

func PrepareForAmbient(cap Capability) error {
	if err := AddPermitted(cap); err != nil {
		return fmt.Errorf("add permitted: %w", err)
	}

	if err := AddInheritable(cap); err != nil {
		return fmt.Errorf("add inheritable: %w", err)
	}

	return nil
}

func AddPermitted(cap Capability) error {
	return capSet(func(_ *unix.CapUserHeader, d []unix.CapUserData) {
		idx := cap / 32
		shift := cap % 32
		d[idx].Permitted |= 1 << shift
	})
}

func AddEffective(cap Capability) error {
	return capSet(func(_ *unix.CapUserHeader, d []unix.CapUserData) {
		idx := cap / 32
		shift := cap % 32
		d[idx].Effective |= 1 << shift
	})
}

func AddInheritable(cap Capability) error {
	return capSet(func(_ *unix.CapUserHeader, d []unix.CapUserData) {
		idx := cap / 32
		shift := cap % 32
		d[idx].Inheritable |= 1 << shift
	})
}

func capSet(mutator func(*unix.CapUserHeader, []unix.CapUserData)) error {
	hdr := &unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0, // self
	}

	data := make([]unix.CapUserData, 2)

	if err := unix.Capget(hdr, &data[0]); err != nil {
		return err
	}

	mutator(hdr, data)

	if err := unix.Capset(hdr, &data[0]); err != nil {
		return err
	}

	return nil
}

func setBit(mask *uint32, cap Capability) {
	*mask |= 1 << uint(cap)
}

func EnableAmbient(cap Capability) error {
	if err := PrepareForAmbient(cap); err != nil {
		return err
	}
	if err := RaiseAmbient(cap); err != nil {
		return err
	}
	return nil
}

// to do

// log

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
		CAP_CHOWN:            "CAP_CHOWN",
		CAP_DAC_OVERRIDE:     "CAP_DAC_OVERRIDE",
		CAP_DAC_READ_SEARCH:  "CAP_DAC_READ_SEARCH",
		CAP_FOWNER:           "CAP_FOWNER",
		CAP_FSETID:           "CAP_FSETID",
		CAP_KILL:             "CAP_KILL",
		CAP_SETGID:           "CAP_SETGID",
		CAP_SETUID:           "CAP_SETUID",
		CAP_SETPCAP:          "CAP_SETPCAP",
		CAP_LINUX_IMMUTABLE:  "CAP_LINUX_IMMUTABLE",
		CAP_NET_BIND_SERVICE: "CAP_NET_BIND_SERVICE",
		CAP_NET_BROADCAST:    "CAP_NET_BROADCAST",
		CAP_NET_ADMIN:        "CAP_NET_ADMIN",
		CAP_NET_RAW:          "CAP_NET_RAW",
		CAP_IPC_LOCK:         "CAP_IPC_LOCK",
		CAP_IPC_OWNER:        "CAP_IPC_OWNER",
		CAP_SYS_MODULE:       "CAP_SYS_MODULE",
		CAP_SYS_RAWIO:        "CAP_SYS_RAWIO",
		CAP_SYS_CHROOT:       "CAP_SYS_CHROOT",
		CAP_SYS_PTRACE:       "CAP_SYS_PTRACE",
		CAP_SYS_PACCT:        "CAP_SYS_PACCT",
		CAP_SYS_ADMIN:        "CAP_SYS_ADMIN",
		CAP_SYS_BOOT:         "CAP_SYS_BOOT",
		CAP_SYS_NICE:         "CAP_SYS_NICE",
		CAP_SYS_RESOURCE:     "CAP_SYS_RESOURCE",
		CAP_SYS_TIME:         "CAP_SYS_TIME",
		CAP_SYS_TTY_CONFIG:   "CAP_SYS_TTY_CONFIG",
		CAP_MKNOD:            "CAP_MKNOD",
		CAP_LEASE:            "CAP_LEASE",
		CAP_AUDIT_WRITE:      "CAP_AUDIT_WRITE",
		CAP_AUDIT_CONTROL:    "CAP_AUDIT_CONTROL",
		CAP_SETFCAP:          "CAP_SETFCAP",
		CAP_MAC_OVERRIDE:     "CAP_MAC_OVERRIDE",
		CAP_MAC_ADMIN:        "CAP_MAC_ADMIN",
		CAP_SYSLOG:           "CAP_SYSLOG",
		CAP_WAKE_ALARM:       "CAP_WAKE_ALARM",
		CAP_BLOCK_SUSPEND:    "CAP_BLOCK_SUSPEND",
		CAP_AUDIT_READ:       "CAP_AUDIT_READ",
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

// improve later with llm maybe

func ExplainCap(cap Capability) CapEffect {
	e := CapEffect{Cap: cap}

	switch cap {

	case CAP_SYS_ADMIN:
		e.Enables = []string{
			"mount and remount filesystems",
			"pivot_root and namespace escape primitives",
			"load/unload filesystem types",
			"setns into arbitrary namespaces",
			"bypass many LSM checks (de facto root)",
		}
		e.Disables = []string{
			"filesystem and namespace confinement",
		}

	case CAP_NET_ADMIN:
		e.Enables = []string{
			"create raw sockets",
			"configure interfaces and routing",
			"add iptables / nftables rules",
			"network namespace escape primitives",
		}

	case CAP_NET_BIND_SERVICE:
		e.Enables = []string{
			"bind to privileged ports (<1024)",
		}

	case CAP_SYS_PTRACE:
		e.Enables = []string{
			"attach to arbitrary processes",
			"read/write process memory",
			"credential theft via ptrace",
		}
		e.Disables = []string{
			"process isolation",
		}

	case CAP_SETUID:
		e.Enables = []string{
			"change UID arbitrarily",
			"assume identity of other users",
		}

	case CAP_SETGID:
		e.Enables = []string{
			"change GID arbitrarily",
			"group-based privilege escalation",
		}

	case CAP_DAC_OVERRIDE:
		e.Enables = []string{
			"bypass file permission checks",
		}

	case CAP_SYS_CHROOT:
		e.Enables = []string{
			"call chroot (not real isolation)",
		}

	default:
		e.Enables = []string{
			"no high-risk primitive documented",
		}
	}

	return e
}

func SetupCapabilities() {
	cap := CAP_NET_BIND_SERVICE
	//_ = DropCapability(cap) // DROP
	DropAllExcept(cap)
	SetCapabilities(CAP_NET_BIND_SERVICE, CAP_SYS_ADMIN)
	_ = ClearAmbient()
	_ = AddInheritable(cap)
	_ = RaiseAmbient(cap)
}
