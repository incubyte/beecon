package memory

import (
	"context"
	"sort"
	"sync"

	"beecon/internal/catalog"
)

// ActivatedDefinitionRepository is an in-memory catalog.ActivatedDefinitions
// for tests (PD65).
type ActivatedDefinitionRepository struct {
	mu             sync.RWMutex
	byProviderSlug map[string]catalog.ActivatedDefinition
}

var _ catalog.ActivatedDefinitions = (*ActivatedDefinitionRepository)(nil)

func NewActivatedDefinitionRepository() *ActivatedDefinitionRepository {
	return &ActivatedDefinitionRepository{byProviderSlug: map[string]catalog.ActivatedDefinition{}}
}

func (r *ActivatedDefinitionRepository) Save(_ context.Context, activated catalog.ActivatedDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byProviderSlug[activated.ProviderSlug] = activated
	return nil
}

func (r *ActivatedDefinitionRepository) FindByProviderSlug(_ context.Context, providerSlug string) (*catalog.ActivatedDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	activated, ok := r.byProviderSlug[providerSlug]
	if !ok {
		return nil, nil
	}
	copied := activated
	return &copied, nil
}

// Delete removes providerSlug's row entirely (Slice 4, PD66): Activate's own
// rollback path uses this to undo a persisted row it just wrote when a
// later step in the same activation fails and this provider had never been
// activated before — a no-op deleting zero rows is not an error.
func (r *ActivatedDefinitionRepository) Delete(_ context.Context, providerSlug string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byProviderSlug, providerSlug)
	return nil
}

func (r *ActivatedDefinitionRepository) ListAll(_ context.Context) ([]catalog.ActivatedDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]catalog.ActivatedDefinition, 0, len(r.byProviderSlug))
	for _, activated := range r.byProviderSlug {
		items = append(items, activated)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProviderSlug < items[j].ProviderSlug })
	return items, nil
}
