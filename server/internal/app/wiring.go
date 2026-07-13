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

	"beecon/internal/access"
	accessbun "beecon/internal/access/driven/bun"
	accesshttp "beecon/internal/access/driving/httpapi"
	"beecon/internal/catalog"
	catalogbun "beecon/internal/catalog/driven/bun"
	cataloghttp "beecon/internal/catalog/driving/httpapi"
	"beecon/internal/config"
	"beecon/internal/connections"
	connectionsbun "beecon/internal/connections/driven/bun"
	"beecon/internal/connections/driven/oauthhttp"
	connectionshttp "beecon/internal/connections/driving/httpapi"
	"beecon/internal/connectweb"
	"beecon/internal/db"
	"beecon/internal/execution"
	"beecon/internal/execution/driven/providerhttp"
	executionhttp "beecon/internal/execution/driving/httpapi"
	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/logging"
	loggingbun "beecon/internal/logging/driven/bun"
	logginghttp "beecon/internal/logging/driving/httpapi"
	"beecon/internal/organizations"
	orgsbun "beecon/internal/organizations/driven/bun"
	orgshttp "beecon/internal/organizations/driving/httpapi"
	"beecon/internal/vault"
)

// Deps are the externally supplied dependencies main.go hands to Wire.
// ProviderDefinitions overrides the embedded provider definitions Wire would
// otherwise load — nil in production; tests use it to point the Outlook
// definition's OAuth endpoints at a fake Microsoft/Graph httptest server. Now
// overrides every module's clock — nil in production (falls back to
// systemNow); tests use it to travel time past a connect link's TTL, a
// connection's token expiry, or an api-key rotation's overlap window without
// a real sleep.
type Deps struct {
	Config              *config.Config
	Logger              *slog.Logger
	ProviderDefinitions []catalog.ProviderDefinition
	Now                 func() time.Time
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

	now := deps.Now
	if now == nil {
		now = systemNow
	}

	providerDefinitions := deps.ProviderDefinitions
	if providerDefinitions == nil {
		loaded, err := catalog.DefaultProviderDefinitions()
		if err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("load provider definitions: %w", err)
		}
		providerDefinitions = loaded
	}

	encryptionKey, err := config.DecodeEncryptionKey(deps.Config.EncryptionKey)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("token encryption key: %w", err)
	}
	tokenVault, err := vault.NewVault(encryptionKey)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("token encryption key: %w", err)
	}

	errorRenderer := httpx.NewErrorRenderer(deps.Logger)
	organizationsFacade := buildOrganizationsFacade(database, now)
	organizationsHandler := orgshttp.NewHandler(organizationsFacade, errorRenderer)
	accessFacade := buildAccessFacade(database, now)
	accessHandler := accesshttp.NewHandler(accessFacade, errorRenderer)
	catalogFacade := buildCatalogFacade(database, providerDefinitions, tokenVault, now)
	if err := catalogFacade.EncryptPlaintextClientSecrets(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("encrypt plaintext client secrets: %w", err)
	}
	catalogHandler := cataloghttp.NewHandler(catalogFacade, errorRenderer)
	loggingFacade := buildLoggingFacade(database, now)
	loggingHandler := logginghttp.NewHandler(loggingFacade, errorRenderer)
	connectionsFacade := buildConnectionsFacade(
		database,
		deps.Config.BaseURL,
		organizationsFacade,
		catalogFacade,
		tokenVault,
		oauthhttp.NewClient(nil),
		connectionsLogRecorder{logs: loggingFacade},
		now,
	)
	connectionsHandler := connectionshttp.NewHandler(connectionsFacade, errorRenderer)
	connectWebHandler, err := connectweb.NewHandler(connectionsFacade)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("parse connect-page templates: %w", err)
	}
	executionFacade := buildExecutionFacade(catalogFacade, connectionsFacade, providerhttp.NewClient(nil), executionLogRecorder{logs: loggingFacade}, now)
	executionHandler := executionhttp.NewHandler(executionFacade, errorRenderer)

	router := buildRouter(
		deps.Config,
		database,
		organizationsHandler,
		accessHandler,
		catalogHandler,
		connectionsHandler,
		connectWebHandler,
		executionHandler,
		loggingHandler,
		accessFacade.Verify,
	)

	return &Wired{
		Router: router,
		DB:     database,
		Close:  database.Close,
	}, nil
}

func buildOrganizationsFacade(database *upstreambun.DB, now func() time.Time) *organizations.Facade {
	repo := orgsbun.NewRepository(database)
	return organizations.NewFacade(repo, repo, idgen.Prefixed("org_"), idgen.Prefixed("user_"), now)
}

func buildAccessFacade(database *upstreambun.DB, now func() time.Time) *access.Facade {
	repo := accessbun.NewRepository(database)
	return access.NewFacade(repo, repo, idgen.Prefixed("key_"), now)
}

func buildCatalogFacade(database *upstreambun.DB, definitions []catalog.ProviderDefinition, tokenVault *vault.Vault, now func() time.Time) *catalog.Facade {
	repo := catalogbun.NewRepository(database)
	return catalog.NewFacade(repo, definitions, idgen.Prefixed("intg_"), now, tokenVault)
}

func buildConnectionsFacade(
	database *upstreambun.DB,
	baseURL string,
	organizationsFacade *organizations.Facade,
	catalogFacade *catalog.Facade,
	tokenVault *vault.Vault,
	oauthClient connections.OAuthClient,
	recorder connections.Recorder,
	now func() time.Time,
) *connections.Facade {
	repo := connectionsbun.NewRepository(database)
	return connections.NewFacade(
		repo,
		repo,
		organizationsFacade,
		organizationsFacade,
		catalogFacade,
		catalogFacade,
		tokenVault,
		oauthClient,
		recorder,
		idgen.Prefixed("conn_"),
		idgen.Prefixed(""),
		idgen.Prefixed(""),
		baseURL,
		now,
	)
}

func buildLoggingFacade(database *upstreambun.DB, now func() time.Time) *logging.Facade {
	repo := loggingbun.NewRepository(database)
	return logging.NewFacade(repo, idgen.Prefixed("log_"), now)
}

func buildExecutionFacade(
	catalogFacade *catalog.Facade,
	connectionsFacade *connections.Facade,
	provider execution.ProviderClient,
	recorder execution.Recorder,
	now func() time.Time,
) *execution.Facade {
	return execution.NewFacade(catalogFacade, connectionsFacade, provider, recorder, now)
}
