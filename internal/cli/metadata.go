package cli

import (
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/metadata"
)

func (a *App) runMetadata(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("metadata requires a subcommand")
	}
	switch args[0] {
	case "serve":
		flags := failFlagSet("metadata serve")
		var listenAddress string
		var metadataFile string
		flags.StringVar(&listenAddress, "listen", "", "listen address")
		flags.StringVar(&metadataFile, "metadata-file", "", "metadata file path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return metadata.Serve(listenAddress, metadataFile)
	default:
		return fmt.Errorf("unknown metadata subcommand %q", args[0])
	}
}
