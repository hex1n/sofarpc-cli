package cli

import (
	"fmt"

	"github.com/hex1n/sofa-rpcctl/greenfield/internal/config"
	"github.com/hex1n/sofa-rpcctl/greenfield/internal/model"
)

func (a *App) runContext(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("context subcommand required: set, list, use, show, delete")
	}
	switch args[0] {
	case "set":
		return a.runContextSet(args[1:])
	case "list":
		return a.runContextList()
	case "use":
		return a.runContextUse(args[1:])
	case "show":
		return a.runContextShow(args[1:])
	case "delete":
		return a.runContextDelete(args[1:])
	default:
		return fmt.Errorf("unknown context subcommand %q", args[0])
	}
}

func (a *App) runContextSet(args []string) error {
	flags := failFlagSet("context set")
	var contextValue model.Context
	flags.StringVar(&contextValue.DirectURL, "direct-url", "", "direct target")
	flags.StringVar(&contextValue.RegistryAddress, "registry-address", "", "registry address")
	flags.StringVar(&contextValue.RegistryProtocol, "registry-protocol", "", "registry protocol")
	flags.StringVar(&contextValue.Protocol, "protocol", "bolt", "SOFARPC protocol")
	flags.StringVar(&contextValue.Serialization, "serialization", "hessian2", "serialization")
	flags.StringVar(&contextValue.UniqueID, "unique-id", "", "service uniqueId")
	flags.IntVar(&contextValue.TimeoutMS, "timeout-ms", 3000, "invoke timeout in milliseconds")
	flags.IntVar(&contextValue.ConnectTimeoutMS, "connect-timeout-ms", 1000, "connect timeout in milliseconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	positionals := flags.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("context set requires a single context name")
	}
	contextValue.Name = positionals[0]
	switch {
	case contextValue.DirectURL != "":
		contextValue.Mode = model.ModeDirect
	case contextValue.RegistryAddress != "":
		contextValue.Mode = model.ModeRegistry
	default:
		return fmt.Errorf("context set requires either --direct-url or --registry-address")
	}
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return err
	}
	store.Contexts[contextValue.Name] = contextValue
	if store.Active == "" {
		store.Active = contextValue.Name
	}
	return config.SaveContextStore(a.Paths, store)
}

func (a *App) runContextList() error {
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, store)
}

func (a *App) runContextUse(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("context use requires exactly one context name")
	}
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return err
	}
	if _, ok := store.Contexts[args[0]]; !ok {
		return fmt.Errorf("context %q does not exist", args[0])
	}
	store.Active = args[0]
	return config.SaveContextStore(a.Paths, store)
}

func (a *App) runContextShow(args []string) error {
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return err
	}
	name := store.Active
	if len(args) == 1 {
		name = args[0]
	}
	value, ok := store.Contexts[name]
	if !ok {
		return fmt.Errorf("context %q does not exist", name)
	}
	return printJSON(a.Stdout, value)
}

func (a *App) runContextDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("context delete requires exactly one context name")
	}
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return err
	}
	delete(store.Contexts, args[0])
	if store.Active == args[0] {
		store.Active = ""
	}
	return config.SaveContextStore(a.Paths, store)
}
