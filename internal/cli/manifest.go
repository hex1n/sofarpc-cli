package cli

import (
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func (a *App) runManifest(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("manifest subcommand required: init, generate")
	}
	switch args[0] {
	case "init":
		return a.runManifestInit(args[1:])
	case "generate":
		return a.runManifestGenerate(args[1:])
	default:
		return fmt.Errorf("unknown manifest subcommand %q", args[0])
	}
}

func (a *App) runManifestInit(args []string) error {
	flags := failFlagSet("manifest init")
	output := ""
	service := "com.example.UserService"
	method := "getUser"
	types := "java.lang.Long"
	payloadMode := model.PayloadRaw
	directURL := ""
	flags.StringVar(&output, "output", "sofarpc.manifest.json", "output file")
	flags.StringVar(&service, "service", service, "service name")
	flags.StringVar(&method, "method", method, "method name")
	flags.StringVar(&types, "types", types, "comma-separated parameter types")
	flags.StringVar(&payloadMode, "payload-mode", payloadMode, "payload mode")
	flags.StringVar(&directURL, "direct-url", "", "default direct url")
	if err := flags.Parse(args); err != nil {
		return err
	}
	target := defaultsTarget()
	target.Mode = model.ModeDirect
	target.DirectURL = directURL
	manifest := model.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget:  target,
		Services: map[string]model.ServiceConfig{
			service: {
				Methods: map[string]model.MethodConfig{
					method: {
						ParamTypes:  parseCSV(types),
						PayloadMode: payloadMode,
					},
				},
			},
		},
	}
	return config.SaveManifest(resolveManifestPath(a.Cwd, output), manifest)
}

func (a *App) runManifestGenerate(args []string) error {
	flags := failFlagSet("manifest generate")
	output := ""
	contextName := ""
	service := ""
	method := ""
	types := ""
	payloadMode := model.PayloadRaw
	stubPathCSV := ""
	flags.StringVar(&output, "output", "sofarpc.manifest.json", "output file")
	flags.StringVar(&contextName, "context", "", "source context name")
	flags.StringVar(&service, "service", "", "service name")
	flags.StringVar(&method, "method", "", "method name")
	flags.StringVar(&types, "types", "", "comma-separated parameter types")
	flags.StringVar(&payloadMode, "payload-mode", payloadMode, "payload mode")
	flags.StringVar(&stubPathCSV, "stub-path", "", "comma-separated stub paths")
	if err := flags.Parse(args); err != nil {
		return err
	}
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return err
	}
	name := firstNonEmpty(contextName, store.Active)
	contextValue := store.Contexts[name]
	manifest := model.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultContext: name,
		DefaultTarget: model.TargetConfig{
			Mode:             contextValue.Mode,
			DirectURL:        contextValue.DirectURL,
			RegistryAddress:  contextValue.RegistryAddress,
			RegistryProtocol: contextValue.RegistryProtocol,
			Protocol:         contextValue.Protocol,
			Serialization:    contextValue.Serialization,
			UniqueID:         contextValue.UniqueID,
			TimeoutMS:        contextValue.TimeoutMS,
			ConnectTimeoutMS: contextValue.ConnectTimeoutMS,
		},
		StubPaths: parseCSV(stubPathCSV),
	}
	if service != "" && method != "" {
		manifest.Services = map[string]model.ServiceConfig{
			service: {
				UniqueID: contextValue.UniqueID,
				Methods: map[string]model.MethodConfig{
					method: {
						ParamTypes:  parseCSV(types),
						PayloadMode: payloadMode,
					},
				},
			},
		}
	}
	return config.SaveManifest(resolveManifestPath(a.Cwd, output), manifest)
}
