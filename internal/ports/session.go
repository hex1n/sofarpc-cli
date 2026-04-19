package ports

import (
	"context"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

type SessionStore interface {
	Save(context.Context, model.WorkspaceSession) error
	Get(context.Context, string) (model.WorkspaceSession, bool, error)
	Delete(context.Context, string) error
	List(context.Context) ([]model.WorkspaceSession, error)
}
