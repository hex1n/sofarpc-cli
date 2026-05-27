package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const resourceMIMEJSON = "application/json"

type sessionResource struct {
	ID          string        `json:"id"`
	ProjectRoot string        `json:"projectRoot,omitempty"`
	Target      target.Config `json:"target,omitempty"`
	CreatedAt   string        `json:"createdAt,omitempty"`
	LastPlan    *planRef      `json:"lastPlan,omitempty"`
	ContractURI string        `json:"contractUri,omitempty"`
	SessionURI  string        `json:"sessionUri"`
	Description string        `json:"description"`
}

type planResource struct {
	SessionID string      `json:"sessionId"`
	Plan      invoke.Plan `json:"plan"`
}

type contractResource struct {
	SessionID   string         `json:"sessionId"`
	ProjectRoot string         `json:"projectRoot,omitempty"`
	Contract    ContractBanner `json:"contract"`
}

type planRef struct {
	URI     string `json:"uri"`
	Service string `json:"service,omitempty"`
	Method  string `json:"method,omitempty"`
}

func registerResources(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sessions := opts.Sessions
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
		URITemplate: "sofarpc://session/{sessionId}",
		Name:        "sofarpc_session",
		Title:       "SOFARPC Session",
		Description: "Read a bounded summary for an in-memory SOFARPC MCP session.",
		MIMEType:    resourceMIMEJSON,
	}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		uri := req.Params.URI
		sessionID, ok := parseSessionResourceURI(uri, "")
		if !ok {
			return nil, sdkmcp.ResourceNotFoundError(uri)
		}
		session, ok := sessions.Get(sessionID)
		if !ok {
			return nil, sdkmcp.ResourceNotFoundError(uri)
		}
		return jsonResource(uri, sessionResourceFromSession(session))
	})

	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
		URITemplate: "sofarpc://session/{sessionId}/plan",
		Name:        "sofarpc_session_plan",
		Title:       "SOFARPC Session Plan",
		Description: "Read the last invocation plan captured for a SOFARPC MCP session.",
		MIMEType:    resourceMIMEJSON,
	}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		uri := req.Params.URI
		sessionID, ok := parseSessionResourceURI(uri, "plan")
		if !ok {
			return nil, sdkmcp.ResourceNotFoundError(uri)
		}
		session, ok := sessions.Get(sessionID)
		if !ok || session.LastPlan == nil {
			return nil, sdkmcp.ResourceNotFoundError(uri)
		}
		return jsonResource(uri, planResource{SessionID: session.ID, Plan: *session.LastPlan})
	})

	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
		URITemplate: "sofarpc://session/{sessionId}/contract",
		Name:        "sofarpc_session_contract",
		Title:       "SOFARPC Session Contract",
		Description: "Read the contract availability banner for a SOFARPC MCP session.",
		MIMEType:    resourceMIMEJSON,
	}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		uri := req.Params.URI
		sessionID, ok := parseSessionResourceURI(uri, "contract")
		if !ok {
			return nil, sdkmcp.ResourceNotFoundError(uri)
		}
		session, ok := sessions.Get(sessionID)
		if !ok {
			return nil, sdkmcp.ResourceNotFoundError(uri)
		}
		snapshot := holder.ForProject(session.ProjectRoot)
		return jsonResource(uri, contractResource{
			SessionID:   session.ID,
			ProjectRoot: session.ProjectRoot,
			Contract:    buildContractBannerForSnapshot(snapshot),
		})
	})
}

func sessionResourceFromSession(session Session) sessionResource {
	out := sessionResource{
		ID:          session.ID,
		ProjectRoot: session.ProjectRoot,
		Target:      session.Target,
		CreatedAt:   session.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		ContractURI: sessionContractResourceURI(session.ID),
		SessionURI:  sessionResourceURI(session.ID),
		Description: "In-memory session state. Sessions expire with the MCP server process and may be evicted by TTL or capacity limits.",
	}
	if session.LastPlan != nil {
		out.LastPlan = &planRef{
			URI:     sessionPlanResourceURI(session.ID),
			Service: session.LastPlan.Service,
			Method:  session.LastPlan.Method,
		}
	}
	return out
}

func jsonResource(uri string, value any) (*sdkmcp.ReadResourceResult, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal resource %s: %w", uri, err)
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{{
			URI:      uri,
			MIMEType: resourceMIMEJSON,
			Text:     string(body),
		}},
	}, nil
}

func sessionResourceURI(sessionID string) string {
	return "sofarpc://session/" + url.PathEscape(sessionID)
}

func sessionPlanResourceURI(sessionID string) string {
	return sessionResourceURI(sessionID) + "/plan"
}

func sessionContractResourceURI(sessionID string) string {
	return sessionResourceURI(sessionID) + "/contract"
}

func sessionResourceLink(sessionID string) *sdkmcp.ResourceLink {
	return &sdkmcp.ResourceLink{
		URI:         sessionResourceURI(sessionID),
		Name:        "sofarpc_session",
		Title:       "SOFARPC Session",
		Description: "Read the current SOFARPC MCP session summary.",
		MIMEType:    resourceMIMEJSON,
	}
}

func sessionPlanResourceLink(sessionID string) *sdkmcp.ResourceLink {
	return &sdkmcp.ResourceLink{
		URI:         sessionPlanResourceURI(sessionID),
		Name:        "sofarpc_session_plan",
		Title:       "SOFARPC Session Plan",
		Description: "Read the last invocation plan captured for this session.",
		MIMEType:    resourceMIMEJSON,
	}
}

func sessionContractResourceLink(sessionID string) *sdkmcp.ResourceLink {
	return &sdkmcp.ResourceLink{
		URI:         sessionContractResourceURI(sessionID),
		Name:        "sofarpc_session_contract",
		Title:       "SOFARPC Session Contract",
		Description: "Read contract availability for this session.",
		MIMEType:    resourceMIMEJSON,
	}
}

func openResourceLinks(sessionID string) []sdkmcp.Content {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return []sdkmcp.Content{
		sessionResourceLink(sessionID),
		sessionContractResourceLink(sessionID),
	}
}

func planCaptureResourceLinks(sessionID string, capture *PlanCaptureResult) []sdkmcp.Content {
	if strings.TrimSpace(sessionID) == "" || capture == nil || !capture.Captured {
		return nil
	}
	return []sdkmcp.Content{sessionPlanResourceLink(sessionID)}
}

func replayResourceLinks(sessionID, source string) []sdkmcp.Content {
	if strings.TrimSpace(sessionID) == "" || source != "session" {
		return nil
	}
	return []sdkmcp.Content{sessionPlanResourceLink(sessionID)}
}

func parseSessionResourceURI(rawURI, suffix string) (string, bool) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme != "sofarpc" || parsed.Host != "session" {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if suffix == "" {
		if len(parts) != 1 {
			return "", false
		}
	} else if len(parts) != 2 || parts[1] != suffix {
		return "", false
	}
	sessionID, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return "", false
	}
	return sessionID, true
}
