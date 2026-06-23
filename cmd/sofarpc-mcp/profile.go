package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/projectbootstrap"
)

// runProfile dispatches the `profile` command group. Today it owns a single
// subcommand, `use`, which persists the project's defaultProfile.
func runProfile(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sofarpc-mcp profile use <name> [--project-root DIR] [--dry-run]")
	}
	switch args[0] {
	case "use":
		return runProfileUse(args[1:])
	default:
		return fmt.Errorf("unknown profile subcommand %q; supported: use", args[0])
	}
}

// runProfileUse implements `profile use <name>`: it records defaultProfile=name
// in .sofarpc/config.local.json after verifying the profile is defined in
// either config file. The profile name is the first positional argument; flags
// follow it.
func runProfileUse(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: sofarpc-mcp profile use <name> [--project-root DIR] [--dry-run]")
	}
	name := strings.TrimSpace(args[0])

	flags := flag.NewFlagSet("profile use", flag.ContinueOnError)
	var projectRoot string
	var dryRun bool
	flags.StringVar(&projectRoot, "project-root", "", "project root (defaults to the current directory)")
	flags.BoolVar(&dryRun, "dry-run", false, "print the would-be change without writing")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: sofarpc-mcp profile use <name> [flags]\n\nRecords defaultProfile in .sofarpc/config.local.json.\n\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("profile use accepts a single profile name; unexpected extra arguments: %s", strings.Join(flags.Args(), " "))
	}

	root, err := resolveProjectSetupRoot(projectRoot)
	if err != nil {
		return err
	}
	result, err := projectbootstrap.UseProfile(projectbootstrap.UseProfileInput{
		ProjectRoot: root,
		Name:        name,
		DryRun:      dryRun,
	})
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Printf("[dry-run] profile use %q → %s:\n%s", name, result.ConfigPath, result.ConfigBody)
		if result.Gitignore != nil {
			if result.Gitignore.WouldChange {
				fmt.Printf("[dry-run] project %s append:\n%s\n", result.Gitignore.Path, result.Gitignore.Entry)
			} else {
				fmt.Printf("[dry-run] project %s already contains %s\n", result.Gitignore.Path, result.Gitignore.Entry)
			}
		}
		return nil
	}
	if result.Gitignore != nil && result.Gitignore.Changed {
		fmt.Printf("project: ensured %s ignores %s\n", result.Gitignore.Path, result.Gitignore.Entry)
	}
	fmt.Printf("project: set defaultProfile=%q in %s\n", name, result.ConfigPath)
	return nil
}
