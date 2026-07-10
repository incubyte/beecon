package organizations

import "context"

// Repository is the organizations module's driven port. FindByID returns
// (nil, nil) on a miss; the facade translates that into ErrNotFound.
type Repository interface {
	Save(ctx context.Context, org Organization) error
	FindByID(ctx context.Context, id OrgID) (*Organization, error)
}
