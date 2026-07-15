// Package app is Beecon's composition root: it wires each module's repository
// to its facade to its handler, and assembles the chi router. cmd/beecon's
// main.go is the only caller.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
	accessbun "beecon/internal/access/driven/bun"
	accesshttp "beecon/internal/access/driving/httpapi"
	"beecon/internal/adminui"
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
	"beecon/internal/delivery"
	deliverybun "beecon/internal/delivery/driven/bun"
	"beecon/internal/delivery/driven/webhookhttp"
	deliveryhttp "beecon/internal/delivery/driving/httpapi"
	"beecon/internal/execution"
	executionbun "beecon/internal/execution/driven/bun"
	"beecon/internal/execution/driven/filestore"
	"beecon/internal/execution/driven/providerhttp"
	executionhttp "beecon/internal/execution/driving/httpapi"
	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/logging"
	loggingbun "beecon/internal/logging/driven/bun"
	logginghttp "beecon/internal/logging/driving/httpapi"
	"beecon/internal/metrics"
	"beecon/internal/organizations"
	orgsbun "beecon/internal/organizations/driven/bun"
	orgshttp "beecon/internal/organizations/driving/httpapi"
	"beecon/internal/triggers"
	triggersbun "beecon/internal/triggers/driven/bun"
	triggershttp "beecon/internal/triggers/driving/httpapi"
	"beecon/internal/vault"
	"beecon/internal/worker"
)

// Deps are the externally supplied dependencies main.go hands to Wire.
// ProviderDefinitions overrides the embedded provider definitions Wire would
// otherwise load — nil in production; tests use it to point the Outlook
// definition's OAuth endpoints at a fake Microsoft/Graph httptest server. Now
// overrides every module's clock — nil in production (falls back to
// systemNow); tests use it to travel time past a connect link's TTL, a
// connection's token expiry, or an api-key rotation's overlap window without
// a real sleep. Sleep overrides the execution facade's retry-loop sleep
// (PD21, Slice 6) — nil in production (execution.NewFacade already defaults
// to a real timer); rate-limit journeys inject a recording no-op sleep so
// callWithRetry's Retry-After/backoff waits run — and can be asserted on —
// without a real delay.
type Deps struct {
	Config              *config.Config
	Logger              *slog.Logger
	ProviderDefinitions []catalog.ProviderDefinition
	Now                 func() time.Time
	Sleep               func(ctx context.Context, d time.Duration) error
}

// Wired is the fully assembled application: the router main.go serves, the
// live DB handle, the background worker.Group (Wire never starts it — see
// Workers' own doc comment), and a Close func for graceful shutdown.
type Wired struct {
	Router  chi.Router
	DB      *upstreambun.DB
	Workers *worker.Group
	Close   func() error
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
	metricsRegistry := metrics.New()
	organizationsFacade := buildOrganizationsFacade(database, now)
	retentionDays := retentionDaysOrDefault(deps.Config.RetentionDays)
	organizationsHandler := orgshttp.NewHandler(organizationsFacade, errorRenderer).
		WithInstallationDefaultRetentionDays(retentionDays)
	accessFacade := buildAccessFacade(database, tokenVault, now)
	accessHandler := accesshttp.NewHandler(accessFacade, errorRenderer)
	operatorFacade := buildOperatorFacade(database, sessionTTLOrDefault(deps.Config.SessionTTL), now).
		WithLoginThrottle(loginMaxAttemptsOrDefault(deps.Config.LoginMaxAttempts), loginLockoutOrDefault(deps.Config.LoginLockout))
	secureCookies := config.SecureCookies(deps.Config.BaseURL)
	operatorHandler := accesshttp.NewOperatorHandler(operatorFacade, errorRenderer, secureCookies)
	catalogFacade := buildCatalogFacade(database, providerDefinitions, tokenVault, now, organizationsFacade)
	if err := catalogFacade.EncryptPlaintextClientSecrets(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("encrypt plaintext client secrets: %w", err)
	}
	catalogHandler := cataloghttp.NewHandler(catalogFacade, errorRenderer)

	// retention is the Slice 7/PD44 adapter both loggingFacade and
	// deliveryFacade's own purge workers read their per-org effective window
	// through — one concrete type satisfying both narrow
	// logging.RetentionReader/delivery.RetentionReader ports structurally
	// (app/retention.go).
	retention := retentionReader{organizations: organizationsFacade, installationDefaultDays: retentionDays}

	loggingFacade := buildLoggingFacade(database, now).WithRetention(retention)
	loggingHandler := logginghttp.NewHandler(loggingFacade, errorRenderer)

	// deliveryFacade is built ahead of connectionsFacade (rather than where
	// Phase 3 Slice 3 first introduced it, further down) so
	// buildConnectionsFacade can wire connections' own connection.expired
	// emission (Slice 5, FD1) straight to it — connectionsFacade never
	// imports delivery itself (BOUNDARIES); this is the same
	// consumer-defined-port-plus-app-adapter seam triggersEventSink already
	// uses.
	deliveryFacade := buildDeliveryFacade(
		database,
		accessFacade,
		deliveryLogRecorder(loggingFacade),
		deliveryTimeoutOrDefault(deps.Config.DeliveryTimeout),
		webhookEndpointCapOrDefault(deps.Config.WebhookEndpointCap),
		endpointAutoDisableFailuresOrDefault(deps.Config.EndpointAutoDisableFailures),
		now,
		metricsRegistry,
	).WithRetention(retention)

	// Slice 9 (PD46): organizationsFacade's config export/import reaches
	// delivery's webhook endpoints and catalog's installed integrations only
	// through these two consumer-defined-port adapters — organizations
	// itself imports neither module (BOUNDARIES). Wired here, after both
	// catalogFacade and deliveryFacade exist; organizationsHandler already
	// holds the same *organizations.Facade pointer these calls mutate in
	// place, so construction order relative to this wiring doesn't matter.
	organizationsFacade.
		WithEndpointPorter(endpointPorterAdapter{delivery: deliveryFacade}).
		WithIntegrationChecker(catalogIntegrationChecker{catalog: catalogFacade})

	connectionsFacade := buildConnectionsFacade(
		database,
		deps.Config.BaseURL,
		organizationsFacade,
		catalogFacade,
		tokenVault,
		oauthhttp.NewClient(nil),
		connectionsLogRecorder(loggingFacade),
		deliveryFacade,
		refreshLeadOrDefault(deps.Config.RefreshLead),
		reconcileIntervalOrDefault(deps.Config.ReconcileInterval),
		now,
	).WithMetrics(metricsRegistry)
	connectionsHandler := connectionshttp.NewHandler(connectionsFacade, errorRenderer)
	connectWebHandler, err := connectweb.NewHandler(connectionsFacade)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("parse connect-page templates: %w", err)
	}
	adminUIHandler, err := adminui.Handler()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("build admin ui handler: %w", err)
	}
	executionFacade := buildExecutionFacade(catalogFacade, connectionsFacade, providerhttp.NewClient(nil), executionLogRecorder(loggingFacade), now).WithMetrics(metricsRegistry)
	if deps.Sleep != nil {
		executionFacade = executionFacade.WithSleep(deps.Sleep)
	}
	fileStore, err := buildFileStore(deps.Config.FilesDir)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("file store: %w", err)
	}
	executionFacade = executionFacade.WithFiles(executionbun.NewFilesRepository(database), fileStore, fileMaxBytesOrDefault(deps.Config.FileMaxBytes), idgen.Prefixed("file_"))
	executionFacade = executionFacade.WithTriggerDefinitions(catalogFacade)
	executionHandler := executionhttp.NewHandler(executionFacade, errorRenderer)
	filesHandler := executionhttp.NewFilesHandler(executionFacade, errorRenderer, deps.Config.BaseURL)

	triggersFacade := buildTriggersFacade(
		database,
		catalogFacade,
		connectionsFacade,
		executionFacade,
		deliveryFacade,
		triggersLogRecorder(loggingFacade),
		pollMinIntervalOrDefault(deps.Config.PollMinInterval),
		now,
	).WithMetrics(metricsRegistry)
	connectionsFacade.WithDependents(triggersDependents{triggers: triggersFacade})
	triggersHandler := triggershttp.NewHandler(triggersFacade, errorRenderer)

	// PD38d's connections-by-status and outbox metrics gauges are registered
	// here, rather than inside metrics.New(), because their GaugeFuncs need
	// a live reference to connectionsFacade/deliveryFacade — neither exists
	// yet when New() builds the registry (architecture doc, Slice 7).
	metricsRegistry.RegisterConnectionsByStatusGauge(countConnectionsByStatus(connectionsFacade))
	metricsRegistry.RegisterOutboxGauges(deliveryFacade.OutboxPendingDepth, deliveryFacade.OutboxOldestPendingAge)

	workers := buildWorkers(
		deps.Logger,
		deliveryFacade,
		triggersFacade,
		connectionsFacade,
		loggingFacade,
		refreshScanIntervalOrDefault(deps.Config.RefreshScanInterval),
		reconcileIntervalOrDefault(deps.Config.ReconcileInterval),
		purgeIntervalOrDefault(deps.Config.PurgeInterval),
	)
	deliveryFacade.WithNudge(func() { workers.Nudge(dispatcherLoopName) })
	deliveryHandler := deliveryhttp.NewHandler(deliveryFacade, errorRenderer)

	router := buildRouter(
		deps.Config,
		database,
		organizationsHandler,
		accessHandler,
		catalogHandler,
		connectionsHandler,
		connectWebHandler,
		adminUIHandler,
		executionHandler,
		filesHandler,
		loggingHandler,
		triggersHandler,
		deliveryHandler,
		operatorHandler,
		metricsRegistry.Handler(),
		metricsRegistry.SummaryHandler(),
		accessFacade.Verify,
		accessFacade.VerifyUserToken,
		operatorFacade.VerifySession,
		operatorFacade.OperatorsExist,
		deps.Logger,
	)

	return &Wired{
		Router:  router,
		DB:      database,
		Workers: workers,
		Close: func() error {
			workers.Stop(context.Background())
			return database.Close()
		},
	}, nil
}

// countConnectionsByStatus adapts connectionsFacade.CountByStatus to the
// plain string-keyed function metrics.Registry.RegisterConnectionsByStatusGauge
// expects (PD38d): metrics imports no domain module (BOUNDARIES), so the
// composition root — which already depends on every module — is where
// connections.Status becomes a plain string.
func countConnectionsByStatus(connectionsFacade *connections.Facade) func(ctx context.Context) (map[string]int, error) {
	return func(ctx context.Context) (map[string]int, error) {
		counts, err := connectionsFacade.CountByStatus(ctx)
		if err != nil {
			return nil, err
		}
		byStatus := make(map[string]int, len(counts))
		for status, count := range counts {
			byStatus[string(status)] = count
		}
		return byStatus, nil
	}
}

func buildOrganizationsFacade(database *upstreambun.DB, now func() time.Time) *organizations.Facade {
	repo := orgsbun.NewRepository(database)
	return organizations.NewFacade(repo, repo, repo, idgen.Prefixed("org_"), idgen.Prefixed("user_"), now)
}

func buildAccessFacade(database *upstreambun.DB, secretVault *vault.Vault, now func() time.Time) *access.Facade {
	repo := accessbun.NewRepository(database)
	signingSecrets := accessbun.NewSigningSecretRepository(database)
	webhookSecrets := accessbun.NewWebhookSecretRepository(database)
	return access.NewFacade(repo, repo, repo, signingSecrets, signingSecrets, webhookSecrets, secretVault, idgen.Prefixed("key_"), idgen.Prefixed("sks_"), idgen.Prefixed("usk_"), idgen.Prefixed("whs_"), now)
}

// buildOperatorFacade wires the operator-auth facade (PD49/PD58, Phase 5
// Slice 1) with its own bun-backed operator/session repositories —
// installation-level, so no organizations dependency here at all.
func buildOperatorFacade(database *upstreambun.DB, sessionTTL time.Duration, now func() time.Time) *access.OperatorFacade {
	operators := accessbun.NewOperatorRepository(database)
	sessions := accessbun.NewOperatorSessionRepository(database)
	return access.NewOperatorFacade(operators, sessions, idgen.Prefixed("op_"), idgen.Prefixed("opsess_"), now, sessionTTL)
}

// sessionTTLOrDefault falls back to config.DefaultSessionTTLSeconds when
// configured is unset (zero) — config.Load already applies this default, but
// Deps.Config may also be built directly (tests) without going through Load.
func sessionTTLOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultSessionTTLSeconds * time.Second
	}
	return configured
}

// loginMaxAttemptsOrDefault falls back to config.DefaultLoginMaxAttempts when
// configured is unset (zero or negative) — config.Load already applies this
// default, but Deps.Config may also be built directly (tests) without going
// through Load.
func loginMaxAttemptsOrDefault(configured int) int {
	if configured <= 0 {
		return config.DefaultLoginMaxAttempts
	}
	return configured
}

// loginLockoutOrDefault falls back to config.DefaultLoginLockoutSeconds when
// configured is unset (zero or negative).
func loginLockoutOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultLoginLockoutSeconds * time.Second
	}
	return configured
}

// buildCatalogFacade wires the catalog facade with its bun repository and
// organizationsFacade as the GovernanceReader (Slice 5, PD42): catalog
// already depends on organizations, so the concrete facade is passed
// directly — no consumer-defined-port-plus-app-adapter indirection needed.
func buildCatalogFacade(database *upstreambun.DB, definitions []catalog.ProviderDefinition, tokenVault *vault.Vault, now func() time.Time, organizationsFacade *organizations.Facade) *catalog.Facade {
	repo := catalogbun.NewRepository(database)
	return catalog.NewFacade(repo, definitions, idgen.Prefixed("intg_"), now, tokenVault, organizationsFacade)
}

// buildConnectionsFacade wires the connections facade with its bun
// repository (which doubles as the installation-level RefreshQueue, Phase 3
// Slice 5 — mirrors delivery's own Repository/WorkQueue split on one
// concrete adapter), the narrow cross-module reader ports it depends on
// (BOUNDARIES: connections depends on organizations and catalog), and
// connections' own connection.expired emission
// (connectionsEventSink, app/recorders.go) wired straight to deliveryFacade —
// connections itself never imports delivery.
func buildConnectionsFacade(
	database *upstreambun.DB,
	baseURL string,
	organizationsFacade *organizations.Facade,
	catalogFacade *catalog.Facade,
	tokenVault *vault.Vault,
	oauthClient connections.OAuthClient,
	recorder connections.Recorder,
	deliveryFacade *delivery.Facade,
	refreshLead time.Duration,
	reconcileInterval time.Duration,
	now func() time.Time,
) *connections.Facade {
	repo := connectionsbun.NewRepository(database)
	facade := connections.NewFacade(
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
	return facade.
		WithScheduling(repo, connectionsEventSink{delivery: deliveryFacade}, refreshLead, reconcileInterval).
		WithStatusCounter(repo)
}

// refreshLeadOrDefault falls back to config.DefaultRefreshLeadSeconds when
// configured is unset (zero) — config.Load already applies this default, but
// Deps.Config may also be built directly (tests) without going through Load.
func refreshLeadOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultRefreshLeadSeconds * time.Second
	}
	return configured
}

// refreshScanIntervalOrDefault falls back to
// config.DefaultRefreshScanIntervalSeconds when configured is unset (zero).
func refreshScanIntervalOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultRefreshScanIntervalSeconds * time.Second
	}
	return configured
}

// reconcileIntervalOrDefault falls back to
// config.DefaultReconcileIntervalSeconds when configured is unset (zero).
func reconcileIntervalOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultReconcileIntervalSeconds * time.Second
	}
	return configured
}

// retentionDaysOrDefault falls back to config.DefaultRetentionDays when
// configured is unset (zero or negative) — config.Load already applies this
// default, but Deps.Config may also be built directly (tests) without going
// through Load.
func retentionDaysOrDefault(configured int) int {
	if configured <= 0 {
		return config.DefaultRetentionDays
	}
	return configured
}

// purgeIntervalOrDefault falls back to config.DefaultPurgeIntervalSeconds
// when configured is unset (zero).
func purgeIntervalOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultPurgeIntervalSeconds * time.Second
	}
	return configured
}

// buildTriggersFacade wires the triggers facade with its bun repository and
// the narrow cross-module reader ports it depends on (BOUNDARIES: triggers
// depends on connections and catalog): catalogFacade satisfies
// DefinitionReader, connectionsFacade satisfies ConnectionReader. Polling
// (Slice 4, PD34) is wired in the same call: the bun Repository doubles as
// the installation-level PollQueue (mirrors delivery's own
// Repository/WorkQueue split on one concrete adapter), executionFacade and
// deliveryFacade reach PollOnce only through the RecordSource/EventSink
// adapters in app/recorders.go (BOUNDARIES: no new import edges).
func buildTriggersFacade(
	database *upstreambun.DB,
	catalogFacade *catalog.Facade,
	connectionsFacade *connections.Facade,
	executionFacade *execution.Facade,
	deliveryFacade *delivery.Facade,
	recorder triggers.Recorder,
	pollMinInterval time.Duration,
	now func() time.Time,
) *triggers.Facade {
	repo := triggersbun.NewRepository(database)
	facade := triggers.NewFacade(repo, catalogFacade, connectionsFacade, idgen.Prefixed("trg_"), now)
	return facade.WithPolling(
		repo,
		executionRecordSource{execution: executionFacade},
		triggersEventSink{delivery: deliveryFacade},
		recorder,
		pollMinInterval,
	)
}

// pollMinIntervalOrDefault falls back to config.DefaultPollMinIntervalSeconds
// when configured is unset (zero) — config.Load already applies this
// default, but Deps.Config may also be built directly (tests) without going
// through Load.
func pollMinIntervalOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultPollMinIntervalSeconds * time.Second
	}
	return configured
}

// buildDeliveryFacade wires the delivery facade with its bun repository
// (also its installation-level WorkQueue — the same Repository implements
// both), accessFacade as the narrow SecretIssuer port (BOUNDARIES: delivery
// depends on access), the real webhookhttp.EndpointCaller, the
// delivery-attempt log recorder, and Slice 8/PD45's two new config values
// (the per-org endpoint cap and the auto-disable failure threshold).
func buildDeliveryFacade(
	database *upstreambun.DB,
	accessFacade *access.Facade,
	recorder delivery.Recorder,
	deliveryTimeout time.Duration,
	webhookEndpointCap int,
	endpointAutoDisableFailures int,
	now func() time.Time,
	metricsRegistry *metrics.Registry,
) *delivery.Facade {
	repo := deliverybun.NewRepository(database)
	facade := delivery.NewFacade(
		repo, repo, accessFacade, webhookhttp.NewClient(nil), recorder,
		idgen.Prefixed("wep_"), idgen.Prefixed("evt_"),
		deliveryTimeout, webhookEndpointCap, endpointAutoDisableFailures,
		now,
	)
	return facade.WithOutboxStats(repo).WithMetrics(metricsRegistry)
}

// deliveryTimeoutOrDefault falls back to config.DefaultDeliveryTimeoutSeconds
// when configured is unset (zero) — config.Load already applies this
// default, but Deps.Config may also be built directly (tests) without going
// through Load.
func deliveryTimeoutOrDefault(configured time.Duration) time.Duration {
	if configured <= 0 {
		return config.DefaultDeliveryTimeoutSeconds * time.Second
	}
	return configured
}

// webhookEndpointCapOrDefault falls back to config.DefaultWebhookEndpointCap
// when configured is unset (zero or negative) — config.Load already applies
// this default, but Deps.Config may also be built directly (tests) without
// going through Load.
func webhookEndpointCapOrDefault(configured int) int {
	if configured <= 0 {
		return config.DefaultWebhookEndpointCap
	}
	return configured
}

// endpointAutoDisableFailuresOrDefault falls back to
// config.DefaultEndpointAutoDisableFailures when configured is unset (zero
// or negative) — config.Load already applies this default, but Deps.Config
// may also be built directly (tests) without going through Load.
func endpointAutoDisableFailuresOrDefault(configured int) int {
	if configured <= 0 {
		return config.DefaultEndpointAutoDisableFailures
	}
	return configured
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

// defaultFilesDir is where local files land when BEECON_FILES_DIR is unset —
// a same-machine deployment (or a test) still boots without one; a
// production installation that cares about files surviving past the host's
// temp-cleanup policy sets BEECON_FILES_DIR explicitly (PD22).
func defaultFilesDir() string {
	return filepath.Join(os.TempDir(), "beecon-files")
}

// buildFileStore builds the execution module's local-disk FileStore (PD22),
// falling back to defaultFilesDir when dir is unset.
func buildFileStore(dir string) (*filestore.Local, error) {
	if dir == "" {
		dir = defaultFilesDir()
	}
	return filestore.NewLocal(dir)
}

// fileMaxBytesOrDefault falls back to config.DefaultFileMaxBytes when
// configured is unset (zero) — config.Load already applies this default, but
// Deps.Config may also be built directly (tests) without going through Load.
func fileMaxBytesOrDefault(configured int64) int64 {
	if configured <= 0 {
		return config.DefaultFileMaxBytes
	}
	return configured
}
