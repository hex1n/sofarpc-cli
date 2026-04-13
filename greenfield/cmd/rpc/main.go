package main

import (
	"fmt"
	"os"

	"github.com/hex1n/sofa-rpcctl/greenfield/internal/cli"
)

func main() {
	app, err := cli.New(os.Stdout, os.Stderr, mustGetwd())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(os.Args[1:]); err != nil {
		if silent, ok := err.(interface{ Silent() bool }); !ok || !silent.Silent() {
			if err.Error() != "" {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		os.Exit(1)
	}
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return cwd
}
