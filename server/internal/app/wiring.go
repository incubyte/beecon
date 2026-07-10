// Package app is Beecon's composition root: it wires each module's repository
// to its facade to its handler, and assembles the chi router. cmd/beecon's
// main.go is the only caller.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	upstreambun "github.com/uptrace/bun"

	"beecon/internal/config"
	"beecon/internal/db"
	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/organizations"
	orgsbun "beecon/internal/organizations/driven/bun"
	orgshttp "beecon/internal/organizations/driving/httpapi"
)

// Deps are the externally supplied dependencies main.go hands to Wire.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
}

// Wired is the fully assembled application: the router main.go serves, the
// live DB handle, and a Close func for graceful shutdown.
type Wired struct {
	Router chi.Router
	DB     *upstreambun.DB
	Close  func() error
}

func systemNow() time.Time { return time.Now().UTC() }

// Wire connects the database, runs boot migrations, builds every module's
// facade and handler, and returns the assembled router.
func Wire(ctx context.Context, deps Deps) (*Wired, error) {
	database, err := db.New(deps.Config.DatabaseDriver, deps.Config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("database unreachable: %w", err)
	}
	if err := db.Migrate(ctx, database); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	errorRenderer := httpx.NewErrorRenderer(deps.Logger)
	organizationsHandler := buildOrganizationsHandler(database, errorRenderer)

	router := buildRouter(deps.Config, database, organizationsHandler)

	return &Wired{
		Router: router,
		DB:     database,
		Close:  database.Close,
	}, nil
}

func buildOrganizationsHandler(database *upstreambun.DB, errorRenderer *httpx.ErrorRenderer) *orgshttp.Handler {
	repo := orgsbun.NewRepository(database)
	facade := organizations.NewFacade(repo, idgen.Prefixed("org_"), systemNow)
	return orgshttp.NewHandler(facade, errorRenderer)
}
