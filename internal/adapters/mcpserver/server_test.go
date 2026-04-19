package mcpserver

import (
	"context"
	"reflect"
	"testing"

	appdescribe "github.com/hex1n/sofarpc-cli/internal/app/describe"
	appfacade "github.com/hex1n/sofarpc-cli/internal/app/facade"
	appinvoke "github.com/hex1n/sofarpc-cli/internal/app/invoke"
	appsession "github.com/hex1n/sofarpc-cli/internal/app/session"
	apptarget "github.com/hex1n/sofarpc-cli/internal/app/target"
	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadeschema"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func TestPlanInvocationDraftUsesFacadeSchemaDetails(t *testing.T) {
	t.Parallel()

	provider := testProvider{
		facade: appfacade.Deps{
			ResolveProjectRoot: func(cwd, project string) (string, error) {
				return project, nil
			},
			LoadConfig: func(projectRoot string, optional bool) (facadeconfig.Config, error) {
				return facadeconfig.Config{
					FacadeModules:   []facadeconfig.FacadeModule{{SourceRoot: "facade/src/main/java"}},
					RequiredMarkers: []string{"required"},
				}, nil
			},
			IterSourceRoots: func(cfg facadeconfig.Config, projectRoot string) []string {
				return []string{"/tmp/project/facade/src/main/java"}
			},
			LoadSemanticRegistry: func(projectRoot string, sourceRoots, markers []string) (facadesemantic.Registry, error) {
				if projectRoot != "/tmp/project" {
					t.Fatalf("projectRoot = %q", projectRoot)
				}
				if !reflect.DeepEqual(sourceRoots, []string{"/tmp/project/facade/src/main/java"}) {
					t.Fatalf("sourceRoots = %v", sourceRoots)
				}
				if !reflect.DeepEqual(markers, []string{"required"}) {
					t.Fatalf("markers = %v", markers)
				}
				return facadesemantic.Registry{}, nil
			},
			BuildMethodSchema: func(registry facadesemantic.Registry, service, method string, preferredParamTypes []string, markers []string) (facadeschema.MethodSchemaEnvelope, error) {
				if service != "com.example.UserFacade" {
					t.Fatalf("service = %q", service)
				}
				if method != "getUser" {
					t.Fatalf("method = %q", method)
				}
				if !reflect.DeepEqual(preferredParamTypes, []string{"com.example.UserRequest"}) {
					t.Fatalf("preferredParamTypes = %v", preferredParamTypes)
				}
				return facadeschema.MethodSchemaEnvelope{
					Service: "com.example.UserFacade",
					Method: facadeschema.MethodSchemaResult{
						Name:           "getUser",
						ParamTypes:     []string{"com.example.UserRequest"},
						ParamsSkeleton: []interface{}{map[string]interface{}{"tenantId": "", "status": "ACTIVE"}},
						ParamsFieldInfo: []facadeschema.ParameterSchema{
							{
								Name:         "request",
								Type:         "com.example.UserRequest",
								RequiredHint: "required request",
								Fields: []facadeschema.FieldSchema{
									{Name: "tenantId", Required: true},
									{Name: "status"},
								},
							},
						},
						ResponseWarning:       "prefer raw mode when wrappers expose Optional getters",
						ResponseWarningReason: "Optional getter on response envelope",
					},
				}, nil
			},
		},
	}
	h := handler{provider: provider}
	session := model.WorkspaceSession{ID: "ws_123", ProjectRoot: "/tmp/project"}
	summary := ResumeContextSummary{
		Service:     "com.example.UserFacade",
		Method:      "getUser",
		ContextName: "dev",
		ParamTypes:  []string{"com.example.UserRequest"},
	}

	args, draft, notes := h.planInvocationDraft(context.Background(), session, summary)

	if len(notes) != 0 {
		t.Fatalf("notes = %v, want empty", notes)
	}
	if draft == nil {
		t.Fatal("draft = nil")
	}
	if draft.ArgsSource != "facade_schema" {
		t.Fatalf("draft.ArgsSource = %q", draft.ArgsSource)
	}
	if len(draft.Parameters) != 1 {
		t.Fatalf("len(draft.Parameters) = %d", len(draft.Parameters))
	}
	if draft.Parameters[0].Name != "request" {
		t.Fatalf("draft.Parameters[0].Name = %q", draft.Parameters[0].Name)
	}
	if draft.Parameters[0].Type != "com.example.UserRequest" {
		t.Fatalf("draft.Parameters[0].Type = %q", draft.Parameters[0].Type)
	}
	if draft.Parameters[0].RequiredHint != "required request" {
		t.Fatalf("draft.Parameters[0].RequiredHint = %q", draft.Parameters[0].RequiredHint)
	}
	if !reflect.DeepEqual(draft.Parameters[0].RequiredFields, []string{"tenantId"}) {
		t.Fatalf("draft.Parameters[0].RequiredFields = %v", draft.Parameters[0].RequiredFields)
	}
	if draft.ResponseWarning == "" {
		t.Fatal("draft.ResponseWarning = empty")
	}
	if draft.ResponseReason == "" {
		t.Fatal("draft.ResponseReason = empty")
	}

	gotArgs, ok := args["args"].([]interface{})
	if !ok {
		t.Fatalf("args[\"args\"] type = %T", args["args"])
	}
	if len(gotArgs) != 1 {
		t.Fatalf("len(args[\"args\"]) = %d", len(gotArgs))
	}
	request, ok := gotArgs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("args[\"args\"][0] type = %T", gotArgs[0])
	}
	if request["tenantId"] != "" || request["status"] != "ACTIVE" {
		t.Fatalf("request skeleton = %#v", request)
	}
	if args["context_name"] != "dev" {
		t.Fatalf("args[\"context_name\"] = %#v", args["context_name"])
	}
	if args["payload_mode"] != model.PayloadRaw {
		t.Fatalf("args[\"payload_mode\"] = %#v", args["payload_mode"])
	}
}

type testProvider struct {
	facade appfacade.Deps
}

func (p testProvider) WorkingDir() string { return "" }

func (p testProvider) ConfigPaths() config.Paths { return config.Paths{} }

func (p testProvider) SessionService() appsession.Deps { return appsession.Deps{} }

func (p testProvider) TargetService() apptarget.Deps { return apptarget.Deps{} }

func (p testProvider) DescribeService() appdescribe.Deps { return appdescribe.Deps{} }

func (p testProvider) InvokeService() appinvoke.Deps { return appinvoke.Deps{} }

func (p testProvider) FacadeService() appfacade.Deps { return p.facade }
