package netdef

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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

// freshInterfaceName creates a unique interface name based on prefix that does
// not collide with any existing interfaces.
func freshInterfaceName(prefix string) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	names := make([]string, len(ifaces))
	for i, iface := range ifaces {
		names[i] = iface.Name
	}
	return freshName(prefix, names), nil
}

var vethRegexp = regexp.MustCompile(`^[0-9]+: ([a-z0-9]+)(@[a-z0-9]+)?:.+`)

// getVethNames is a helper function to poll for veth interfaces.
func getVethNames() ([]string, error) {
	cmd := exec.Command("ip", "link", "show", "type", "veth")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	buf := bytes.NewReader(out)
	scanner := bufio.NewScanner(buf)
	ret := make([]string, 0)
	for scanner.Scan() {
		match := vethRegexp.FindStringSubmatch(scanner.Text())
		if match != nil {
			ret = append(ret, match[1])
		}
	}
	return ret, nil
}

// freshVethName creates a unique veth name based on prefix that does not
// collide with any existing veths.
func freshVethName(prefix string) (string, error) {
	names, err := getVethNames()
	if err != nil {
		return "", err
	}
	return freshName(prefix, names), nil
}

// freshNamespaceName creates a unique netns name that does not collide with any
// exising namespaces.
func freshNamespaceName(prefix string) (string, error) {
	files, err := ioutil.ReadDir("/var/run/netns")
	if err == nil {
		// Successfully opened the directory
		names := make([]string, len(files))
		for i, file := range files {
			if file.IsDir() {
				continue
			}
			names[i] = file.Name()
		}
		return freshName(prefix, names), nil
	} else if os.IsNotExist(err) {
		// Directory doesn't exist, assume there are no namespaces
		names := make([]string, 0)
		return freshName(prefix, names), nil
	} else {
		// Real error
		return "", err
	}
}

// freshName generates a new name based on prefix that does not collide with any
// names in existing.
func freshName(prefix string, existing []string) string {
	found := false
	max := uint64(0)
	for _, name := range existing {
		if strings.HasPrefix(name, prefix) {
			found = true
			numstr := name[len(prefix):]
			num, err := strconv.ParseUint(numstr, 10, 64)
			if err != nil {
				continue
			}
			if num > max {
				max = num
			}
		}
	}
	if found {
		max++
	}
	return fmt.Sprintf("%s%d", prefix, max)
}

func (r *RenderedNetwork) freshNetworkName(name string) (string, error) {
	bridgename, err := r.freshInterfaceName("Bridge")
	if err != nil {
		return "", err
	}
	r.subnets[name] = bridgename
	return bridgename, nil
}

func (r *RenderedNetwork) freshInterfaceName(typ string) (string, error) {
	prefix := r.prefixes[typ]
	bridgename, err := freshInterfaceName(prefix)
	if err != nil {
		return "", err
	}
	return bridgename, nil
}

func (r *RenderedNetwork) freshVethName(typ string) (string, error) {
	prefix := r.prefixes[typ]
	bridgename, err := freshVethName(prefix)
	if err != nil {
		return "", err
	}
	return bridgename, nil
}

// CreateNamespace creates a unique namespace and, if successful, logs a mapping
// of the configuration name to the generated namespace name.
func (r *RenderedNetwork) CreateNamespace(name string) error {
	freshname, err := freshNamespaceName(r.prefixes["Namespace"])
	if err != nil {
		return err
	}
	err = callBin("ip", "netns", "add", freshname)
	if err == nil {
		r.Namespaces[name] = freshname
	}
	return err
}

// DeleteNamespace deletes an internet namespace.
func (r *RenderedNetwork) DeleteNamespace(name string) error {
	err := callBin("ip", "netns", "del", name)
	if err == nil {
		delete(r.Namespaces, name)
	}
	return err
}

// CreateBridge creates a new bridge with openvswitch.
func (r *RenderedNetwork) CreateBridge(name string) error {
	err := callBin("ovs-vsctl", "add-br", name)
	if err == nil {
		r.Bridges[name] = struct{}{}
	}
	return err
}

// DeleteBridge deletes a bridge with openvswitch.
func (r *RenderedNetwork) DeleteBridge(name string) error {
	err := callBin("ovs-vsctl", "del-br", name)
	if err == nil {
		delete(r.Bridges, name)
	}
	return err
}

// BridgeAddPort adds a port to an openvswitch bridge.
func (r *RenderedNetwork) BridgeAddPort(bridge, ifname string) error {
	return callBin("ovs-vsctl", "add-port", bridge, ifname)
}

// PortSetParameter sets a variable for a given port.
func (r *RenderedNetwork) PortSetParameter(port, param, val string) error {
	typeStr := fmt.Sprintf("%s=%s", param, val)
	return callBin("ovs-vsctl", "set", "interface", port, typeStr)
}

// PortSetOption sets an option for a given port.
func (r *RenderedNetwork) PortSetOption(port, option, peer string) error {
	param := fmt.Sprintf("options:%s", option)
	return r.PortSetParameter(port, param, peer)
}

// PatchBridges creates patch ports on two interfaces and peers them,
// effectively connecting two openvswitch bridges.
func (r *RenderedNetwork) PatchBridges(a, b string, l *LinkOpts) error {
	ab, err := r.freshVethName("Port")
	if err != nil {
		return errors.Wrap(err, "creating fresh port name")
	}
	if err = r.CreateVeth(ab); err != nil {
		return errors.Wrap(err, "creating port")
	}
	ba, err := r.freshVethName("Port")
	if err != nil {
		return errors.Wrap(err, "creating fresh port name")
	}
	if err = r.CreateVeth(ba); err != nil {
		return errors.Wrap(err, "creating port")
	}
	if err = r.BridgeAddPort(a, ab); err != nil {
		return errors.Wrap(err, "adding port")
	}
	if err = r.PortSetParameter(ab, "type", "patch"); err != nil {
		return errors.Wrap(err, "configuring port type")
	}
	if err = r.PortSetOption(ab, "peer", ba); err != nil {
		return errors.Wrap(err, "configuring port options")
	}
	if err = r.BridgeAddPort(b, ba); err != nil {
		return errors.Wrap(err, "adding port")
	}
	if err = r.PortSetParameter(ba, "type", "patch"); err != nil {
		return errors.Wrap(err, "configuring port type")
	}
	if err = r.PortSetOption(ba, "peer", ab); err != nil {
		return errors.Wrap(err, "configuring port options")
	}
	if l != nil {
		if err = l.Apply(ab); err != nil {
			return errors.Wrap(err, "setting patch link options")
		}
	}

	return nil
}

// NetNsExec executes a command within a network namespace.
func (r *RenderedNetwork) NetNsExec(ns string, cmdn string, nsargs ...string) error {
	args := []string{"ip", "netns", "exec", ns, cmdn}
	args = append(args, nsargs...)
	return callBin(args...)
}

// SetDev updates the state of a network device.
func (r *RenderedNetwork) SetDev(dev string, state string) error {
	return callBin("ip", "link", "set", "dev", dev, state)
}

// CreateVeth creates a new veth interface.
func (r *RenderedNetwork) CreateVeth(a string) error {
	err := callBin("ip", "link", "add", a, "type", "veth")
	if err == nil {
		r.Interfaces[a] = struct{}{}
	}
	return err
}

// CreateVethPair creates a new pair of veth interfaces that are connected.
func (r *RenderedNetwork) CreateVethPair(a, b string) error {
	err := callBin("ip", "link", "add", a, "type", "veth", "peer", "name", b)
	if err == nil {
		r.Interfaces[a] = struct{}{}
		r.Interfaces[b] = struct{}{}
	}
	return err
}

// DeleteInterface deletes a network interface.
func (r *RenderedNetwork) DeleteInterface(name string) error {
	err := callBin("ip", "link", "del", name)
	if err == nil {
		delete(r.Interfaces, name)
	}
	return err
}

// AssignVethToNamespace moves a veth into a network namespace.
func (r *RenderedNetwork) AssignVethToNamespace(veth, ns string) error {
	err := callBin("ip", "link", "set", veth, "netns", ns)
	if err == nil {
		delete(r.Interfaces, veth)
	}
	return err
}

// Config describes a network configuration. From a Config, netdef can create a
// RenderedNetwork, representing the set of actual namespaces, bridges, and veth
// interfaces created in executing a configuration.
type Config struct {
	// Networks is a slice of descriptions of subnets.
	Networks []Network
	// Peers is a slice of descriptions of peers which will manifest as
	// namespaces with the desired connectivity configuraiton.
	Peers []Peer
	// Prefixes is a user-configurable map of prefixes for the various network
	// constructs created by this library. If it is not provided, the nil value
	// will be replaced with defaults. The valid keys are:
	// - Bridge (default "br")
	// - Interface (default "veth")
	// - Patch (default "patch")
	// - Port (default "tap")
	// - Namespace (default "ns")
	Prefixes map[string]string
}

// Network describes a subnet configuration.
type Network struct {
	// Name of the subnet, used only in configuration, not actual rendering.
	Name string
	// IpRange is a string representation of a class C or D IP range.
	IpRange string
	// Links is a map of subnets this network is connected to to the link
	// options that describe the physical qualities of the link.
	Links map[string]*LinkOpts
	// BindMask is a default subnet mask for all peers created on this network.
	BindMask string

	ipnet  *net.IPNet
	nextIp int64
}

// RenderedNetwork describes the actual changes made to a host operating system
// in executing a configuration. This exists primarily for cleaning up rendered
// network configurations.
type RenderedNetwork struct {
	// Bridges is a set of bridges created by a Config.
	Bridges map[string]struct{}
	// Namespaces is a map of peer names to the namespaces created for them.
	Namespaces map[string]string
	// Interfaces ia set of veths created in the global namespace. Typically
	// these will all be ports to openvswitch bridges.
	Interfaces map[string]struct{}

	subnets  map[string]string
	prefixes map[string]string
}

// NewRenderedNetwork initializes a RenderedNetwork based on the prefixes
// supplied by the Config.
func (c *Config) NewRenderedNetwork() *RenderedNetwork {
	r := &RenderedNetwork{
		Bridges:    make(map[string]struct{}),
		Namespaces: make(map[string]string),
		Interfaces: make(map[string]struct{}),
		subnets:    make(map[string]string),
		prefixes: map[string]string{
			"Bridge":    "br",
			"Interface": "veth",
			"Patch":     "patch",
			"Port":      "tap",
			"Namespace": "ns",
		},
	}

	if c.Prefixes != nil {
		for k, v := range c.Prefixes {
			r.prefixes[k] = v
		}
	}

	return r
}

// GetNextIp returns the next IPv4 address in the Network's IpRange.
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

// Peer describes a peer to be rendered into a network namespace.
type Peer struct {
	// Name of the peer.
	Name string
	// A map of subnets this peer is connected to and their link properties.
	Links map[string]*LinkOpts
	// The default subnet mask for this peer.
	BindMask string
}

// LinkOpts describes a physical network connection.
type LinkOpts struct {
	// Latency of the interface.
	Latency string
	// Jitter of the interface.
	Jitter string
	// Bandwidth available to the interface.
	Bandwidth string
	// PacketLoss rate of the interface.
	PacketLoss string

	lset *ctrlnet.LinkSettings
}

// Parse parses human readable LinkOpts into openvswitch ready LinkSettings.
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

// Apply configures an interface to have the specified settings. It is all or
// nothing, so a user must configure all aspects of the LinkOpts for this method
// to have an effect.
func (lo *LinkOpts) Apply(iface string) error {
	if lo.Bandwidth == "" && lo.PacketLoss == "" && lo.Jitter == "" && lo.Latency == "" {
		return nil
	}

	if lo.lset == nil {
		return fmt.Errorf("linkopts has not been parsed for iface %s", iface)
	}

	return ctrlnet.SetLink(iface, lo.lset)
}

// Create realizes a Config as a RenderedNetwork, tracking the side effects in
// the RenderedNetwork.
func (cfg *Config) Create() (*RenderedNetwork, error) {
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

			if l == nil {
				continue
			}
			if err := l.Parse(); err != nil {
				return nil, err
			}
		}
	}

	for name, net := range nets {
		for targetNet, l := range net.Links {
			if _, ok := nets[targetNet]; !ok {
				return nil, fmt.Errorf("network %s has link to non-existent network %s", name, targetNet)
			}

			if l == nil {
				continue
			}
			if err := l.Parse(); err != nil {
				return nil, err
			}
		}
	}

	r := cfg.NewRenderedNetwork()

	for n := range nets {
		bridgename, err := r.freshNetworkName(n)
		if err != nil {
			return r, errors.Wrap(err, "generating network name")
		}
		if err := r.CreateBridge(bridgename); err != nil {
			return r, errors.Wrap(err, "creating bridge")
		}
	}

	for name, net := range nets {
		bridge := r.subnets[name]
		for targetNet, l := range net.Links {
			targetBridge := r.subnets[targetNet]
			if err := r.PatchBridges(bridge, targetBridge, l); err != nil {
				return r, errors.Wrap(err, "patching bridges")
			}
		}
	}

	for _, p := range cfg.Peers {
		if err := r.CreateNamespace(p.Name); err != nil {
			return r, err
		}
		ns := r.Namespaces[p.Name]

		for net, l := range p.Links {
			bridge := r.subnets[net]
			lnA, err := r.freshVethName("Interface")
			if err != nil {
				return r, errors.Wrap(err, "generate interface name")
			}
			lnB, err := r.freshVethName("Port")
			if err != nil {
				return r, errors.Wrap(err, "generate port name")
			}

			if err := r.CreateVethPair(lnA, lnB); err != nil {
				return r, errors.Wrap(err, "create veth pair")
			}

			if err := r.BridgeAddPort(bridge, lnB); err != nil {
				return r, errors.Wrap(err, "bridge add port")
			}

			if err := r.AssignVethToNamespace(lnA, ns); err != nil {
				return r, errors.Wrap(err, "failed to assign veth to namespace")
			}

			if err := r.NetNsExec(ns, "ip", "link", "set", "dev", "lo", "up"); err != nil {
				return r, errors.Wrap(err, "set ns link up")
			}

			if err := r.NetNsExec(ns, "ip", "link", "set", "dev", lnA, "up"); err != nil {
				return r, errors.Wrap(err, "set ns link up")
			}

			if err := r.SetDev(lnB, "up"); err != nil {
				return r, err
			}

			next, err := nets[net].GetNextIp(p.BindMask)
			if err != nil {
				return r, err
			}

			if err := r.NetNsExec(ns, "ip", "addr", "add", next, "dev", lnA); err != nil {
				return r, err
			}

			if l == nil {
				continue
			}
			if err := l.Apply(lnB); err != nil {
				return r, err
			}
		}
	}

	return r, nil
}

// Cleanup reverses the changes made by calling Create on a Config.
func (r *RenderedNetwork) Cleanup() error {
	for iface := range r.Interfaces {
		if err := r.DeleteInterface(iface); err != nil {
			return err
		}
	}

	for _, ns := range r.Namespaces {
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
				Links: map[string]*LinkOpts{
					"homenetwork": nil,
				},
			},
		},
		Peers: []Peer{
			{
				Name: "c1",
				Links: map[string]*LinkOpts{
					"homenetwork": &LinkOpts{},
				},
				BindMask: "255.255.0.0",
			},
			{
				Name: "c2",
				Links: map[string]*LinkOpts{
					"officenetwork": &LinkOpts{
						Latency: "50ms",
					},
				},
			},
		},
	}

	r, err := cfg.Create()
	if err != nil {
		panic(err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()

	if err := r.Cleanup(); err != nil {
		panic(err)
	}
}
