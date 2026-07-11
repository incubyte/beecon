package organizations

import "context"

// Repository is the organizations module's driven port for the
// installation-level Organization entity. FindByID returns (nil, nil) on a
// miss; the facade translates that into ErrNotFound. Organization lookup is
// installation-level, not org-scoped — there is no wider scope to filter by.
type Repository interface {
	Save(ctx context.Context, org Organization) error
	FindByID(ctx context.Context, id OrgID) (*Organization, error)
}

// UserRepository is the organizations module's driven port for the
// org-scoped User entity. Every method takes the owning OrgID as its second
// parameter, so a query without org scope cannot be expressed. FindUserByID
// returns (nil, nil) on a miss (including a user that belongs to a different
// organization); the facade translates that into ErrUserNotFound.
type UserRepository interface {
	SaveUser(ctx context.Context, user User) error
	FindUserByID(ctx context.Context, org OrgID, id UserID) (*User, error)
}
