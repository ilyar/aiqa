package extsuite

import (
	"context"

	"github.com/ilyar/aiqa/internal/api"
)

// InventoryContext applies operator-configured limits only to SDK inventory
// probes, not to unrelated model responses. Zero-valued runtime fields retain
// the same conservative defaults as the CLI.
func (r RuntimeServices) InventoryContext(ctx context.Context) context.Context {
	models, bytes := r.MaxCatalogModels, r.InventoryByteBudget
	if models <= 0 {
		models = 4096
	}
	if bytes <= 0 {
		bytes = 16 << 20
	}
	return api.WithJSONCollectionLimit(api.WithResponseBodyLimit(ctx, bytes), models)
}
