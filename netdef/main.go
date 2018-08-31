package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli"
	"github.com/whyrusleeping/go-netdef"
)

func readConfig(path string) (*netdef.Config, error) {
	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	cfg := &netdef.Config{}
	if err = json.NewDecoder(fi).Decode(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func writeRender(path string, r *netdef.RenderedNetwork) error {
	fi, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fi.Close()
	if err := json.NewEncoder(fi).Encode(r); err != nil {
		return err
	}
	return nil
}

func main() {
	app := cli.NewApp()

	create := cli.Command{
		Name: "create",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "output",
				Value: "config.render.json",
				Usage: "Path to write out the rendered configuration",
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().First() == "" {
				return fmt.Errorf("must specify netdef configuration file")
			}

			cfg, err := readConfig(c.Args().First())
			if err != nil {
				return err
			}

			r, err := cfg.Create()
			if err != nil {
				return err
			}

			err = writeRender(c.String("output"), r)
			if err != nil {
				return err
			}

			return nil
		},
	}

	cleanup := cli.Command{
		Name: "cleanup",
		Action: func(c *cli.Context) error {
			if c.Args().First() == "" {
				return fmt.Errorf("must specify netdef configuration file")
			}

			fi, err := os.Open(c.Args().First())
			if err != nil {
				return err
			}
			defer fi.Close()

			var r netdef.RenderedNetwork
			if err := json.NewDecoder(fi).Decode(&r); err != nil {
				return err
			}

			if err := r.Cleanup(); err != nil {
				return err
			}

			return nil
		},
	}

	app.Commands = []cli.Command{
		create,
		cleanup,
	}

	app.RunAndExitOnError()
}
