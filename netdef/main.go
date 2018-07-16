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
		Action: func(c *cli.Context) error {
			if c.Args().First() == "" {
				return fmt.Errorf("must specify netdef configuration file")
			}

			fi, err := os.Open(c.Args().First())
			if err != nil {
				return err
			}
			defer fi.Close()

			var cfg netdef.Config
			if err := json.NewDecoder(fi).Decode(&cfg); err != nil {
				return err
			}

			if err := netdef.Create(&cfg); err != nil {
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

			var cfg netdef.Config
			if err := json.NewDecoder(fi).Decode(&cfg); err != nil {
				return err
			}

			if err := netdef.Cleanup(&cfg); err != nil {
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
