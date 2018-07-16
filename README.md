# netdef

A virtual network management tool.

## Installation
To install the cli tool, just run:
```
go get -u github.com/whyrusleeping/go-netdef/netdef
```

## Dependencies
This only runs on linux right now, and requires a working installation of `openvswitch`.

## Usage
Netdef can be used as a package, but it also provides a command line tool
that operates on json formatted 'netdef' specifications. An example such file might look like:

```json
{
	"networks": [{
		"name":"seattle",
		"iprange": "10.1.1.0/24"
	}],
	"peers": [
		{
			"name":"wolf",
			"links": {
				"seattle": {},
			}
		},
		{
			"name":"bear",
			"links": {
				"seattle": {
					"latency": "10ms"
				}
			}
		}
	],
}
```

This specification defines a network named 'seattle' that allocates ip
addresses for its peers in the 10.1.1.0/24 subnet, and two peers named 'wolf'
and 'bear'. It then defines that 'wolf' has a link to 'seattle' with default
settings, and that 'bear' has a link to 'seattle' with a 10ms latency on it.

To create this network, save the json to a file `example.nd` and run:
```
sudo netdef create example.nd
```

This will set up all the namespaces needed and switches, and connect them as described.
Once setup, you can run commands on a given peer by doing:
```
sudo ip netns exec wolf ping 10.1.1.2
```

To teardown the network, run:
```
sudo netdef cleanup example.nd
```

## TODO
Theres a lot more I want to do here, this is a partial list (roughly in order of priority):
- [ ] Actually implement latencies/bandwidth/packet-loss with go-ctrlnet
- [ ] Better permissions (running everything as root sucks)
- [ ] Network to network links
- [ ] Better validation of config
- [ ] Multi-host namespaces (via openvswitch)
- [ ] Different 'bridge' implementations (i.e. brctl)

## License
MIT
