package netdef

import (
	"bufio"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/whyrusleeping/go-ctrlnet"
)

func callBin(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(strings.TrimRight(string(out), "\n"))
	}

	return nil

}

func (r *RenderedNetwork) CreateNamespace(name string) error {
	err := callBin("ip", "netns", "add", name)
	if err == nil {
		r.Namespaces[name] = struct{}{}
	}
	return err
}

func (r *RenderedNetwork) DeleteNamespace(name string) error {
	err := callBin("ip", "netns", "del", name)
	if err == nil {
		delete(r.Namespaces, name)
	}
	return err
}

func (r *RenderedNetwork) CreateBridge(name string) error {
	err := callBin("ovs-vsctl", "add-br", name)
	if err == nil {
		r.Bridges[name] = struct{}{}
	}
	return err
}

func (r *RenderedNetwork) DeleteBridge(name string) error {
	err := callBin("ovs-vsctl", "del-br", name)
	if err == nil {
		delete(r.Bridges, name)
	}
	return err
}

func (r *RenderedNetwork) BridgeAddPort(bridge, ifname string) error {
	return callBin("ovs-vsctl", "add-port", bridge, ifname)
}

func (r *RenderedNetwork) PortSetParameter(port, param, val string) error {
	typeStr := fmt.Sprintf("%s=%s", param, val)
	return callBin("ovs-vsctl", "set", "interface", port, typeStr)
}

func (r *RenderedNetwork) PortSetOption(port, option, peer string) error {
	param := fmt.Sprintf("options:%s", option)
	return r.PortSetParameter(port, param, peer)
}

func (r *RenderedNetwork) PatchBridges(a, b string) error {
	ab := fmt.Sprintf("p-%s-%s", a, b)
	ba := fmt.Sprintf("p-%s-%s", b, a)
	if err := r.CreateVeth(ab); err != nil {
		return err
	}
	if err := r.CreateVeth(ba); err != nil {
		return err
	}
	if err := r.BridgeAddPort(a, ab); err != nil {
		return err
	}
	if err := r.PortSetParameter(ab, "type", "patch"); err != nil {
		return err
	}
	if err := r.PortSetOption(ab, "peer", ba); err != nil {
		return err
	}
	if err := r.BridgeAddPort(b, ba); err != nil {
		return err
	}
	if err := r.PortSetParameter(ba, "type", "patch"); err != nil {
		return err
	}
	if err := r.PortSetOption(ba, "peer", ab); err != nil {
		return err
	}

	return nil
}

func (r *RenderedNetwork) NetNsExec(ns string, cmdn string, nsargs ...string) error {
	args := []string{"ip", "netns", "exec", ns, cmdn}
	args = append(args, nsargs...)
	return callBin(args...)
}

func (r *RenderedNetwork) SetDev(dev string, state string) error {
	return callBin("ip", "link", "set", "dev", dev, state)
}

func (r *RenderedNetwork) CreateVeth(a string) error {
	err := callBin("ip", "link", "add", a, "type", "veth")
	if err == nil {
		r.Interfaces[a] = struct{}{}
	}
	return err
}

func (r *RenderedNetwork) CreateVethPair(a, b string) error {
	err := callBin("ip", "link", "add", a, "type", "veth", "peer", "name", b)
	if err == nil {
		r.Interfaces[a] = struct{}{}
		r.Interfaces[b] = struct{}{}
	}
	return err
}

func (r *RenderedNetwork) DeleteInterface(name string) error {
	err := callBin("ip", "link", "del", name)
	if err == nil {
		delete(r.Interfaces, name)
	}
	return err
}

func (r *RenderedNetwork) AssignVethToNamespace(veth, ns string) error {
	err := callBin("ip", "link", "set", veth, "netns", ns)
	if err == nil {
		delete(r.Interfaces, veth)
	}
	return err
}

type Config struct {
	Networks []Network
	Peers    []Peer
}

type Network struct {
	Name     string
	IpRange  string
	Links    map[string]LinkOpts
	BindMask string

	ipnet  *net.IPNet
	nextIp int64
}

type RenderedNetwork struct {
	Bridges    map[string]struct{}
	Namespaces map[string]struct{}
	Interfaces map[string]struct{}
}

func (n *Network) GetNextIp(mask string) (string, error) {
	ip := n.ipnet.IP

	// TODO: better algorithm for this all. github.com/apparentlymart/go-cidr looks decent
	n.nextIp++

	ipn := big.NewInt(0).SetBytes([]byte(ip))
	ipn.Add(ipn, big.NewInt(n.nextIp))

	b := ipn.Bytes()
	subnetMask := net.IPMask(net.ParseIP(mask))
	if subnetMask == nil {
		subnetMask = net.IPMask(net.ParseIP(n.BindMask))
		if subnetMask == nil {
			subnetMask = n.ipnet.Mask
		}
	}
	out := net.IPNet{
		IP:   net.IPv4(b[0], b[1], b[2], b[3]),
		Mask: subnetMask,
	}
	return out.String(), nil
}

type Peer struct {
	Name     string
	Links    map[string]LinkOpts
	BindMask string
}

type LinkOpts struct {
	Latency    string
	Jitter     string
	Bandwidth  string
	PacketLoss string

	lset *ctrlnet.LinkSettings
}

func (lo *LinkOpts) Parse() error {
	lo.lset = new(ctrlnet.LinkSettings)

	if lo.Latency != "" {
		lat, err := time.ParseDuration(lo.Latency)
		if err != nil {
			return err
		}

		lo.lset.Latency = uint(lat.Nanoseconds() / 1000000)
	}

	if lo.Jitter != "" {
		jit, err := time.ParseDuration(lo.Jitter)
		if err != nil {
			return err
		}

		lo.lset.Jitter = uint(jit.Nanoseconds() / 1000000)
	}

	bw, err := ParseHumanLinkRate(lo.Bandwidth)
	if err != nil {
		return err
	}
	lo.lset.Bandwidth = bw

	pl, err := ParsePercentage(lo.PacketLoss)
	if err != nil {
		return err
	}

	lo.lset.PacketLoss = uint8(pl)

	return nil
}

func (lo *LinkOpts) Apply(iface string) error {
	if lo.Bandwidth == "" && lo.PacketLoss == "" && lo.Jitter == "" && lo.Latency == "" {
		return nil
	}

	if lo.lset == nil {
		return fmt.Errorf("linkopts has not been parsed")
	}

	return ctrlnet.SetLink(iface, lo.lset)
}

func Create(cfg *Config) (*RenderedNetwork, error) {
	nets := make(map[string]*Network)
	for i := range cfg.Networks {
		n := cfg.Networks[i]
		if _, ok := nets[n.Name]; ok {
			return nil, fmt.Errorf("duplicate network name: %s", n.Name)
		}

		_, ipn, err := net.ParseCIDR(n.IpRange)
		if err != nil {
			return nil, err
		}

		n.ipnet = ipn
		nets[n.Name] = &n
	}

	peers := make(map[string]bool)
	for _, p := range cfg.Peers {
		_, ok := peers[p.Name]
		if ok {
			return nil, fmt.Errorf("duplicate peer name: %s", p.Name)
		}
		peers[p.Name] = true

		for net, l := range p.Links {
			if _, ok := nets[net]; !ok {
				return nil, fmt.Errorf("peer %s has link to non-existent network %q", p.Name, net)
			}

			if err := l.Parse(); err != nil {
				return nil, err
			}
		}
	}

	for name, net := range nets {
		for targetNet := range net.Links {
			if _, ok := nets[targetNet]; !ok {
				return nil, fmt.Errorf("network %s has link to non-existent network %s", name, targetNet)
			}
		}
	}

	r := &RenderedNetwork{
		Namespaces: make(map[string]struct{}),
		Bridges:    make(map[string]struct{}),
		Interfaces: make(map[string]struct{}),
	}

	for n := range nets {
		if err := r.CreateBridge(n); err != nil {
			return r, err
		}
	}

	for name, net := range nets {
		for targetNet := range net.Links {
			if err := r.PatchBridges(name, targetNet); err != nil {
				return r, err
			}
		}
	}

	for _, p := range cfg.Peers {
		if err := r.CreateNamespace(p.Name); err != nil {
			return r, err
		}

		for net, l := range p.Links {
			lnA := "l-" + p.Name + "-" + net
			lnB := "br-" + p.Name + "-" + net

			if err := r.CreateVethPair(lnA, lnB); err != nil {
				return r, errors.Wrap(err, "create veth pair")
			}

			if err := r.BridgeAddPort(net, lnB); err != nil {
				return r, errors.Wrap(err, "bridge add port")
			}

			if err := r.AssignVethToNamespace(lnA, p.Name); err != nil {
				return r, errors.Wrap(err, "failed to assign veth to namespace")
			}

			if err := r.NetNsExec(p.Name, "ip", "link", "set", "dev", "lo", "up"); err != nil {
				return r, errors.Wrap(err, "set ns link up")
			}

			if err := r.NetNsExec(p.Name, "ip", "link", "set", "dev", lnA, "up"); err != nil {
				return r, errors.Wrap(err, "set ns link up")
			}

			if err := r.SetDev(lnB, "up"); err != nil {
				return r, err
			}

			next, err := nets[net].GetNextIp(p.BindMask)
			if err != nil {
				return r, err
			}

			if err := r.NetNsExec(p.Name, "ip", "addr", "add", next, "dev", lnA); err != nil {
				return r, err
			}

			if err := l.Apply(lnA); err != nil {
				fmt.Println(err)
				// return err
			}
		}
	}

	return r, nil
}

func (r *RenderedNetwork) Cleanup() error {
	for iface := range r.Interfaces {
		if err := r.DeleteInterface(iface); err != nil {
			return err
		}
	}

	for ns := range r.Namespaces {
		if err := r.DeleteNamespace(ns); err != nil {
			return err
		}
	}

	for br := range r.Bridges {
		if err := r.DeleteBridge(br); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	cfg := &Config{
		Networks: []Network{
			{
				Name:     "homenetwork",
				IpRange:  "10.1.1.0/24",
				BindMask: "255.255.0.0",
			},
			{
				Name:    "officenetwork",
				IpRange: "10.1.2.0/24",
				Links: map[string]LinkOpts{
					"homenetwork": LinkOpts{},
				},
			},
		},
		Peers: []Peer{
			{
				Name: "c1",
				Links: map[string]LinkOpts{
					"homenetwork": LinkOpts{},
				},
				BindMask: "255.255.0.0",
			},
			{
				Name: "c2",
				Links: map[string]LinkOpts{
					"officenetwork": LinkOpts{
						Latency: "50ms",
					},
				},
			},
		},
	}

	r, err := Create(cfg)
	if err != nil {
		panic(err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()

	if err := r.Cleanup(); err != nil {
		panic(err)
	}
}
