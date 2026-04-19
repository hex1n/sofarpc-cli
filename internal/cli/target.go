package cli

import (
	"fmt"
	"io"
	"strings"

	apptarget "github.com/hex1n/sofarpc-cli/internal/app/target"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

func (a *App) runTarget(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return a.runTargetShow(args)
	}
	switch args[0] {
	case "show":
		return a.runTargetShow(args[1:])
	default:
		return fmt.Errorf("unknown target subcommand %q", args[0])
	}
}

func (a *App) runTargetShow(args []string) error {
	flags := failFlagSet("target show")
	var (
		input   invocationInputs
		project string
		asJSON  bool
		showAll bool
		explain bool
	)
	flags.StringVar(&project, "project", "", "project root used to resolve manifest and project-scoped context")
	flags.StringVar(&input.ManifestPath, "manifest", "", "manifest file path")
	flags.StringVar(&input.ContextName, "context", "", "context name")
	flags.StringVar(&input.Service, "service", "", "optional service name (for manifest uniqueId resolution)")
	flags.StringVar(&input.DirectURL, "direct-url", "", "direct bolt target")
	flags.StringVar(&input.RegistryAddress, "registry-address", "", "registry address")
	flags.StringVar(&input.RegistryProtocol, "registry-protocol", "", "registry protocol")
	flags.StringVar(&input.Protocol, "protocol", "", "SOFARPC protocol")
	flags.StringVar(&input.Serialization, "serialization", "", "serialization")
	flags.StringVar(&input.UniqueID, "unique-id", "", "service uniqueId")
	flags.IntVar(&input.TimeoutMS, "timeout-ms", 0, "invoke timeout in milliseconds")
	flags.IntVar(&input.ConnectTimeoutMS, "connect-timeout-ms", 0, "connect timeout in milliseconds")
	flags.BoolVar(&asJSON, "json", false, "print JSON output")
	flags.BoolVar(&showAll, "all", false, "show all relevant target/context candidates")
	flags.BoolVar(&explain, "explain", false, "show how the final target was selected")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("unknown target show args: %s", strings.Join(flags.Args(), " "))
	}
	report, err := a.resolveTargetReport(project, input, showAll, explain)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(a.Stdout, report)
	}
	return printTargetReport(a.Stdout, report)
}

func (a *App) resolveTargetReport(project string, input invocationInputs, showAll, explain bool) (targetReport, error) {
	return a.newTargetService().Execute(apptarget.Request{
		Cwd:     a.Cwd,
		Paths:   a.Paths,
		Project: project,
		Input:   input,
		ShowAll: showAll,
		Explain: explain,
	})
}

func printTargetReport(out io.Writer, report targetReport) error {
	if _, err := fmt.Fprintf(out, "project root:      %s\n", emptyFallback(report.ProjectRoot, "(not resolved)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "manifest path:     %s\n", emptyFallback(report.ManifestPath, "(not resolved)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "manifest loaded:   %t\n", report.ManifestLoaded); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "active context:    %s\n", emptyFallback(report.ActiveContext, "(none)")); err != nil {
		return err
	}
	if strings.TrimSpace(report.Service) != "" {
		if _, err := fmt.Fprintf(out, "service:           %s\n", report.Service); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "mode:              %s\n", report.Target.Mode); err != nil {
		return err
	}
	switch report.Target.Mode {
	case targetmodel.ModeDirect:
		if _, err := fmt.Fprintf(out, "direct url:        %s\n", emptyFallback(report.Target.DirectURL, "(not set)")); err != nil {
			return err
		}
	case targetmodel.ModeRegistry:
		if _, err := fmt.Fprintf(out, "registry:          %s://%s\n", emptyFallback(report.Target.RegistryProtocol, "(not set)"), emptyFallback(report.Target.RegistryAddress, "(not set)")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "protocol:          %s\n", emptyFallback(report.Target.Protocol, "(not set)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "serialization:     %s\n", emptyFallback(report.Target.Serialization, "(not set)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "uniqueId:          %s\n", emptyFallback(report.Target.UniqueID, "(not set)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "timeout ms:        %d\n", report.Target.TimeoutMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "connect timeout:   %d\n", report.Target.ConnectTimeoutMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "reachable:         %t\n", report.Reachability.Reachable); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "probe target:      %s\n", emptyFallback(report.Reachability.Target, "(none)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "probe message:     %s\n", emptyFallback(report.Reachability.Message, "(none)")); err != nil {
		return err
	}
	if len(report.Candidates) > 0 {
		if _, err := fmt.Fprintln(out, "\ncandidates:"); err != nil {
			return err
		}
		for _, candidate := range report.Candidates {
			marker := " "
			if candidate.Selected {
				marker = "*"
			}
			if _, err := fmt.Fprintf(out, "%s %s [%s]\n", marker, candidate.Name, strings.Join(candidate.Roles, ", ")); err != nil {
				return err
			}
			if candidate.Target.Mode == targetmodel.ModeDirect {
				if _, err := fmt.Fprintf(out, "  direct url: %s\n", emptyFallback(candidate.Target.DirectURL, "(not set)")); err != nil {
					return err
				}
			} else if candidate.Target.Mode == targetmodel.ModeRegistry {
				if _, err := fmt.Fprintf(out, "  registry:   %s://%s\n", emptyFallback(candidate.Target.RegistryProtocol, "(not set)"), emptyFallback(candidate.Target.RegistryAddress, "(not set)")); err != nil {
					return err
				}
			}
		}
	}
	if len(report.Layers) > 0 {
		if _, err := fmt.Fprintln(out, "\nlayers:"); err != nil {
			return err
		}
		for _, layer := range report.Layers {
			if _, err := fmt.Fprintf(out, "- %s [%s]\n", layer.Name, layer.Kind); err != nil {
				return err
			}
			if len(layer.AppliedFields) > 0 {
				if _, err := fmt.Fprintf(out, "  applied fields: %s\n", strings.Join(layer.AppliedFields, ", ")); err != nil {
					return err
				}
			}
			if hasVisibleTargetFields(layer.Target) {
				if err := printIndentedTarget(out, "  ", layer.Target); err != nil {
					return err
				}
			}
		}
	}
	if len(report.Explain) > 0 {
		if _, err := fmt.Fprintln(out, "\nselection:"); err != nil {
			return err
		}
		for _, line := range report.Explain {
			if _, err := fmt.Fprintf(out, "- %s\n", line); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveTargetProjectRoot(cwd, project string) (string, bool, error) {
	root := strings.TrimSpace(project)
	if root != "" {
		validated, err := facadeconfig.ValidateProjectDir(root)
		return validated, true, err
	}
	if abs, err := facadeconfig.ResolveProjectRoot(cwd, nil); err == nil {
		return abs, true, nil
	}
	if abs, err := facadeconfig.ValidateProjectDir(cwd); err == nil {
		return abs, true, nil
	}
	return "", false, nil
}

func hasVisibleTargetFields(target targetmodel.TargetConfig) bool {
	return len(targetFieldNames(target)) > 0
}

func targetFieldNames(target targetmodel.TargetConfig) []string {
	fields := make([]string, 0, 8)
	if strings.TrimSpace(target.Mode) != "" {
		fields = append(fields, "mode")
	}
	if strings.TrimSpace(target.DirectURL) != "" {
		fields = append(fields, "directUrl")
	}
	if strings.TrimSpace(target.RegistryAddress) != "" {
		fields = append(fields, "registryAddress")
	}
	if strings.TrimSpace(target.RegistryProtocol) != "" {
		fields = append(fields, "registryProtocol")
	}
	if strings.TrimSpace(target.Protocol) != "" {
		fields = append(fields, "protocol")
	}
	if strings.TrimSpace(target.Serialization) != "" {
		fields = append(fields, "serialization")
	}
	if strings.TrimSpace(target.UniqueID) != "" {
		fields = append(fields, "uniqueId")
	}
	if target.TimeoutMS > 0 {
		fields = append(fields, "timeoutMs")
	}
	if target.ConnectTimeoutMS > 0 {
		fields = append(fields, "connectTimeoutMs")
	}
	return fields
}

func printIndentedTarget(out io.Writer, indent string, target targetmodel.TargetConfig) error {
	if strings.TrimSpace(target.Mode) != "" {
		if _, err := fmt.Fprintf(out, "%smode: %s\n", indent, target.Mode); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.DirectURL) != "" {
		if _, err := fmt.Fprintf(out, "%sdirect url: %s\n", indent, target.DirectURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.RegistryAddress) != "" || strings.TrimSpace(target.RegistryProtocol) != "" {
		if _, err := fmt.Fprintf(out, "%sregistry: %s://%s\n", indent, emptyFallback(target.RegistryProtocol, "(not set)"), emptyFallback(target.RegistryAddress, "(not set)")); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.Protocol) != "" {
		if _, err := fmt.Fprintf(out, "%sprotocol: %s\n", indent, target.Protocol); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.Serialization) != "" {
		if _, err := fmt.Fprintf(out, "%sserialization: %s\n", indent, target.Serialization); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.UniqueID) != "" {
		if _, err := fmt.Fprintf(out, "%suniqueId: %s\n", indent, target.UniqueID); err != nil {
			return err
		}
	}
	if target.TimeoutMS > 0 {
		if _, err := fmt.Fprintf(out, "%stimeout ms: %d\n", indent, target.TimeoutMS); err != nil {
			return err
		}
	}
	if target.ConnectTimeoutMS > 0 {
		if _, err := fmt.Fprintf(out, "%sconnect timeout: %d\n", indent, target.ConnectTimeoutMS); err != nil {
			return err
		}
	}
	return nil
}
