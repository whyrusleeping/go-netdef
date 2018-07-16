package netdef

import (
	"fmt"
	"math/big"
	"net"
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

func CreateNamespace(name string) error {
	return callBin("ip", "netns", "add", name)
}

func DeleteNamespace(name string) error {
	return callBin("ip", "netns", "del", name)
}

func CreateBridge(name string) error {
	return callBin("ovs-vsctl", "add-br", name)
}

func DeleteBridge(name string) error {
	return callBin("ovs-vsctl", "del-br", name)
}

func BridgeAddPort(bridge, ifname string) error {
	return callBin("ovs-vsctl", "add-port", bridge, ifname)
}

func NetNsExec(ns string, cmdn string, nsargs ...string) error {
	args := []string{"ip", "netns", "exec", ns, cmdn}
	args = append(args, nsargs...)
	return callBin(args...)
}

func SetDev(dev string, state string) error {
	return callBin("ip", "link", "set", "dev", dev, state)
}

func CreateVethPair(a, b string) error {
	return callBin("ip", "link", "add", a, "type", "veth", "peer", "name", b)
}

func DeleteInterface(name string) error {
	return callBin("ip", "link", "del", name)
}

func AssignVethToNamespace(veth, ns string) error {
	return callBin("ip", "link", "set", veth, "netns", ns)
}

type Config struct {
	Networks []Network
	Peers    []Peer
}

type Network struct {
	Name    string
	IpRange string
	ipnet   *net.IPNet
	nextIp  int64
}

func (n *Network) GetNextIp() (string, error) {
	ip := n.ipnet.IP

	// TODO: better algorithm for this all. github.com/apparentlymart/go-cidr looks decent
	n.nextIp++

	ipn := big.NewInt(0).SetBytes([]byte(ip))
	ipn.Add(ipn, big.NewInt(n.nextIp))

	b := ipn.Bytes()
	out := net.IPNet{
		IP:   net.IPv4(b[0], b[1], b[2], b[3]),
		Mask: n.ipnet.Mask,
	}
	return out.String(), nil
}

type Peer struct {
	Name  string
	Links map[string]LinkOpts
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

func Create(cfg *Config) error {
	nets := make(map[string]*Network)
	for _, n := range cfg.Networks {
		if _, ok := nets[n.Name]; ok {
			return fmt.Errorf("duplicate network name: %s", n.Name)
		}

		_, ipn, err := net.ParseCIDR(n.IpRange)
		if err != nil {
			return err
		}

		n.ipnet = ipn
		nets[n.Name] = &n
	}

	peers := make(map[string]bool)
	for _, p := range cfg.Peers {
		_, ok := peers[p.Name]
		if ok {
			return fmt.Errorf("duplicate peer name: %s", p.Name)
		}
		peers[p.Name] = true

		for net, l := range p.Links {
			if _, ok := nets[net]; !ok {
				return fmt.Errorf("peer %s has link to non-existent network %q", p.Name, net)
			}

			if err := l.Parse(); err != nil {
				return err
			}
		}
	}

	for n := range nets {
		if err := CreateBridge(n); err != nil {
			return err
		}
	}

	for _, p := range cfg.Peers {
		if err := CreateNamespace(p.Name); err != nil {
			return err
		}

		for net, l := range p.Links {
			lnA := "l-" + p.Name + "-" + net
			lnB := "br-" + p.Name + "-" + net

			if err := CreateVethPair(lnA, lnB); err != nil {
				return errors.Wrap(err, "create veth pair")
			}

			if err := BridgeAddPort(net, lnB); err != nil {
				return errors.Wrap(err, "bridge add port")
			}

			if err := AssignVethToNamespace(lnA, p.Name); err != nil {
				return errors.Wrap(err, "failed to assign veth to namespace")
			}

			if err := NetNsExec(p.Name, "ip", "link", "set", "dev", lnA, "up"); err != nil {
				return errors.Wrap(err, "set ns link up")
			}

			if err := SetDev(lnB, "up"); err != nil {
				return err
			}

			next, err := nets[net].GetNextIp()
			if err != nil {
				return err
			}

			if err := NetNsExec(p.Name, "ip", "addr", "add", next, "dev", lnA); err != nil {
				return err
			}

			if err := l.Apply(lnA); err != nil {
				return err
			}
		}
	}

	return nil
}

func Cleanup(cfg *Config) error {
	for _, n := range cfg.Networks {
		if err := DeleteBridge(n.Name); err != nil {
			fmt.Println("error deleting bridge: ", err)
		}
	}

	for _, p := range cfg.Peers {
		if err := DeleteNamespace(p.Name); err != nil {
			fmt.Println("error deleting namespace: ", err)
		}

		for net, _ := range p.Links {
			lnA := "l-" + p.Name + "-" + net

			// TODO: check for existence first
			if err := DeleteInterface(lnA); err != nil {
				fmt.Println(err)
			}
		}
	}
	return nil
}

func main() {
	cfg := &Config{
		Networks: []Network{
			{
				Name:    "homenetwork",
				IpRange: "10.1.1.0/24",
			},
		},
		Peers: []Peer{
			{
				Name: "compy1",
				Links: map[string]LinkOpts{
					"homenetwork": LinkOpts{},
				},
			},
			{
				Name: "compy2",
				Links: map[string]LinkOpts{
					"homenetwork": LinkOpts{
						Latency: "50ms",
					},
				},
			},
		},
	}

	if err := Create(cfg); err != nil {
		panic(err)
	}

}
