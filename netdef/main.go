package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli"
	"github.com/whyrusleeping/go-netdef"
)

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

			fi, err := os.Open(c.Args().First())
			if err != nil {
				return err
			}

			var cfg netdef.Config
			if err = json.NewDecoder(fi).Decode(&cfg); err != nil {
				fi.Close()
				return err
			}
			fi.Close()

			r, err := netdef.Create(&cfg)
			if err != nil {
				return err
			}

			fi, err = os.Open(c.String("output"))
			if err != nil {
				return err
			}
			if err := json.NewEncoder(fi).Encode(r); err != nil {
				fi.Close()
				return err
			}
			fi.Close()

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
