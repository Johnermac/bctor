package ntw

import (
	"net"
	"os/exec"

	"github.com/Johnermac/bctor/lib"
	"github.com/vishvananda/netlink"
)

func CleanupContainerNetworking(scx *lib.SupervisorCtx, netres *NetResources) {
	if netres == nil {
		return
	}

	link, err := netlink.LinkByName(netres.HostVeth)
	if err == nil {
		_ = netlink.LinkDel(link)
	}

	// release IP back to allocator
	if netres.IP != "" {
		scx.IPAlloc.Release(net.ParseIP(netres.IP))
	}
}

func (a *IPAllocator) Release(ip net.IP) {
	if ip == nil {
		return
	}
	delete(a.Used, ip.String())
}

// optional
func DeleteBridge(name string) {
	link, err := netlink.LinkByName(name)
	if err == nil {
		_ = netlink.LinkDel(link)
	}
}

func RemoveNATRule(subnet string, outIface string) error {
	cmd := exec.Command(
		"iptables",
		"-t", "nat",
		"-D", "POSTROUTING",
		"-s", subnet,
		"-o", outIface,
		"-j", "MASQUERADE",
	)
	return cmd.Run()
}
