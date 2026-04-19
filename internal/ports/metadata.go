package ports

import (
	"context"
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

type MetadataService interface {
	ResolveServiceSchema(context.Context, string, string, bool) (model.ServiceSchema, string, bool, []string, error)
	ResolveMethod(context.Context, string, string, string, []string, json.RawMessage, bool) (contract.ProjectMethod, string, bool, []string, error)
}
