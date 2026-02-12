package ntw

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/Johnermac/bctor/lib"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

//network

type NetResources struct {
	Bridge     string
	HostVeth   string
	PeerVeth   string
	IP         string
}

type IPAllocator struct {
	Base   net.IP
	Subnet *net.IPNet
	Next   int
	Used   map[string]bool
}



func EnsureBridge(name, cidr string) error {
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
		},
	}

	if err := netlink.LinkAdd(br); err != nil {
		if !errors.Is(err, syscall.EEXIST) {
			return err
		}
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	addr, _ := netlink.ParseAddr(cidr)
	_ = netlink.AddrAdd(link, addr)

	return netlink.LinkSetUp(link)
}

func EnableIPForwarding() error {
	return os.WriteFile(
		"/proc/sys/net/ipv4/ip_forward",
		[]byte("1"),
		0644,
	)
}

func DefaultRouteInterface() (string, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return "", err
	}

	for _, r := range routes {
		// default route has Dst == nil
		if r.Dst == nil && r.Gw != nil {
			link, err := netlink.LinkByIndex(r.LinkIndex)
			if err != nil {
				continue
			}
			return link.Attrs().Name, nil
		}
	}

	return "", fmt.Errorf("no default route found")
}


func AddNATRule(subnet string, outIface string) error {
	cmd := exec.Command(
		"iptables",
		"-t", "nat",
		"-A", "POSTROUTING",
		"-s", subnet,
		"-o", outIface,
		"-j", "MASQUERADE",
	)
	return cmd.Run()
}

func NewIPAlloc(cidr string) (*IPAllocator, error) {
	ip, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	return &IPAllocator{
		Base:   ip.To4(),
		Next:   10,
		Subnet: subnet,
		Used:   make(map[string]bool),
	}, nil
}

func (a *IPAllocator) Allocate() (net.IP, error) {
	for a.Next < 254 {
		ip := make(net.IP, len(a.Base))
		copy(ip, a.Base)
		ip[3] = byte(a.Next)
		a.Next++

		if !a.Used[ip.String()] {
			a.Used[ip.String()] = true
			return ip, nil
		}
	}

	return nil, lib.LogError("no available IPs")
}

// Helper para gerar sufixo aleatório hexadecimal
func randomSuffix(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}



func SetupContainerVeth(
	id string,         // ID do container para logs/identificação
	bridgeName string,
	netnsFD int,       // Use o INT do FD que veio do mapa 'created'
	ip net.IP,
) (*NetResources, error) {

	// 1. Nomes Únicos
	suffix := randomSuffix(2) // 2 bytes = 4 chars hex. Ex: ve-abcd
	hostVeth := fmt.Sprintf("ve-%s", suffix) 
	tempPeer := fmt.Sprintf("vp-%s", suffix)

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: hostVeth},
		PeerName:  tempPeer,
	}

	// 2. Criar Veth Pair
	if err := netlink.LinkAdd(veth); err != nil {
		return nil, fmt.Errorf("LinkAdd failed: %w", err)
	}

	// Função de limpeza em caso de erro nos passos seguintes
	cleanup := func() { _ = netlink.LinkDel(veth) }

	// 3. Setup da Bridge no Host
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("bridge %s not found: %w", bridgeName, err)
	}

	// 4. Attach Host Side
	hostLink, err := netlink.LinkByName(hostVeth)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("host veth lookup failed: %w", err)
	}

	if err := netlink.LinkSetMaster(hostLink, br); err != nil {
		cleanup()
		return nil, fmt.Errorf("LinkSetMaster failed: %w", err)
	}

	if err := netlink.LinkSetUp(hostLink); err != nil {
		cleanup()
		return nil, fmt.Errorf("LinkSetUp host failed: %w", err)
	}

	// 5. Mover Peer para o Namespace do Container
	// IMPORTANTE: Use o FD passado no argumento, sem depender de PID!
	peerLink, err := netlink.LinkByName(tempPeer)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("peer veth lookup failed: %w", err)
	}

	// Move usando o FD direto (netnsFD)
	if err := netlink.LinkSetNsFd(peerLink, netnsFD); err != nil {
		cleanup()
		return nil, fmt.Errorf("LinkSetNsFd failed to move to FD %d: %w", netnsFD, err)
	}

	return &NetResources{
		Bridge:   bridgeName,
		HostVeth: hostVeth,
		PeerVeth: tempPeer, 
		IP:       ip.String(),
	}, nil
}




func ConfigureContainerInterface(
	netnsFD int,        // Recebe o FD diretamente do IPC/Mapa
	tempPeerName string,
	ip net.IP,
	gateway net.IP,
	subnet *net.IPNet,
) error {
	// 1. OBRIGATÓRIO: Trava a thread do SO para evitar que o Go mude de thread
	// durante a troca de namespace.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 2. Salva o namespace original (Host) para poder retornar no final
	origNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get original netns: %w", err)
	}
	defer origNS.Close()

	// 3. ENTRA NO NAMESPACE DO CONTAINER
	// Usamos o FD direto para evitar dependência do /proc/PID
	if err := unix.Setns(netnsFD, unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("setns to container failed: %w", err)
	}
	
	// Garante que a thread volte para o namespace do Host ao final da função
	defer netns.Set(origNS)

	// --- AGORA ESTAMOS DENTRO DO CONTAINER ---

	// 4. Localiza a interface (Retry loop para aguardar a migração do kernel)
	var link netlink.Link
	const maxRetries = 50
	for i := 0; i < maxRetries; i++ {
		link, err = netlink.LinkByName(tempPeerName)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("interface %s not found after migration: %w", tempPeerName, err)
	}

	// 5. RENOMEAR PARA ETH0
	// O Linux exige que a interface esteja em DOWN para renomear
	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("failed to set link down for rename: %w", err)
	}
	if err := netlink.LinkSetName(link, "eth0"); err != nil {
		return fmt.Errorf("failed to rename %s to eth0: %w", tempPeerName, err)
	}

	// Busca o novo handle agora com o nome eth0
	eth0, err := netlink.LinkByName("eth0")
	if err != nil {
		return fmt.Errorf("failed to find eth0 after rename: %w", err)
	}

	// 6. CONFIGURAÇÃO DE IP
	// Verifica se a subnet não é nula para evitar panic
	if subnet == nil {
		return fmt.Errorf("subnet cannot be nil")
	}
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: subnet.Mask,
		},
	}
	if err := netlink.AddrAdd(eth0, addr); err != nil {
		return fmt.Errorf("failed to add IP address %s: %w", ip, err)
	}

	// 7. LEVANTA AS INTERFACES (eth0 e lo)
	if err := netlink.LinkSetUp(eth0); err != nil {
		return fmt.Errorf("failed to set eth0 UP: %w", err)
	}

	if lo, err := netlink.LinkByName("lo"); err == nil {
		_ = netlink.LinkSetUp(lo)
	}

	// 8. CONFIGURA ROTA PADRÃO (GATEWAY)
	route := &netlink.Route{
		LinkIndex: eth0.Attrs().Index,
		Gw:        gateway,
	}
	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add default route via %s: %w", gateway, err)
	}

	return nil
}






func NetworkConfig(netnsFD int, scx *lib.SupervisorCtx, spec *lib.ContainerSpec, created map[lib.NamespaceType]int) *NetResources {
	// 1. Alocação de IP
	ip, err := scx.IPAlloc.Allocate()
	if err != nil || ip == nil {
		lib.LogError("IP allocation failed for %s: %v", spec.ID, err)
		return nil
	}

	// 2. Setup do Veth Pair (Usando o FD do Namespace em vez do PID)
	// IMPORTANTE: Passe o spec.ID para gerar nomes aleatórios ve- e vp-
	//netnsFD := created[lib.NSNet]
	
	netres, err := SetupContainerVeth(
		spec.ID,      // Para nomes únicos
		"bctor0",     // Nome da Bridge
		netnsFD,      // FD do namespace (Muito mais estável que PID)
		ip,
	)
	if err != nil {
		lib.LogError("Veth setup failed: %v", err)
		scx.IPAlloc.Release(ip)
		return nil
	}

	// 3. Configuração interna do Container
	gateway := net.ParseIP("10.0.0.1")
	err = ConfigureContainerInterface(
		netnsFD,           // FD do namespace
		netres.PeerVeth,   // O nome temporário (vp-xxxx)
		ip,
		gateway,
		scx.Subnet,        // O campo que corrigimos para não ser nil
	)
	
	if err != nil {
		lib.LogError("Interface config failed inside container: %v", err)
		scx.IPAlloc.Release(ip)
		// Aqui você deve decidir se remove o veth do host para não sujar
		return nil
	}
	
	netres.IP = ip.String()
	return netres
}
