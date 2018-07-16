# netdef

A virtual network management tool.


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

This specification defines a network named 'seattle', and two peers named
'wolf' and 'bear'. It then defines that 'wolf' has a link to 'seattle' with
default settings, and that 'bear' has a link to 'seattle' with a 10ms latency
on it.

## License
MIT
