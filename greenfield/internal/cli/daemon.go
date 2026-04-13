package cli

import (
	"fmt"
)

func (a *App) runDaemon(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("daemon subcommand required: list, show, stop, prune")
	}
	switch args[0] {
	case "list":
		return a.runDaemonList()
	case "show":
		return a.runDaemonShow(args[1:])
	case "stop":
		return a.runDaemonStop(args[1:])
	case "prune":
		return a.runDaemonPrune()
	default:
		return fmt.Errorf("unknown daemon subcommand %q", args[0])
	}
}

func (a *App) runDaemonList() error {
	daemons, err := a.Runtime.ListDaemons()
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, map[string]any{
		"cacheDir": a.Runtime.DaemonDir(),
		"daemons":  daemons,
	})
}

func (a *App) runDaemonShow(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("daemon show requires exactly one daemon key")
	}
	record, err := a.Runtime.GetDaemon(args[0])
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, record)
}

func (a *App) runDaemonStop(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("daemon stop requires exactly one daemon key")
	}
	action, err := a.Runtime.StopDaemon(args[0])
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, action)
}

func (a *App) runDaemonPrune() error {
	actions, err := a.Runtime.PruneDaemons()
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, map[string]any{
		"cacheDir": a.Runtime.DaemonDir(),
		"removed":  actions,
	})
}
