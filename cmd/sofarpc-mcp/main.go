package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hex1n/sofarpc-cli/internal/cli"
)

func main() {
	app, err := cli.New(os.Stdin, os.Stdout, os.Stderr, mustGetwd())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.RunMCP(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
