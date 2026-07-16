/**
 * Hand-written types mirroring the Go DTOs in
 * server/internal/organizations/driving/httpapi/dto.go (no codegen this
 * phase — see the architecture doc's OpenAPI evolution trigger, §11).
 */

export interface Organization {
  id: string;
  name: string;
  allowedRedirectUris: string[];
  createdAt: string;
}

export interface Page<T> {
  items: T[];
  nextCursor?: string;
}

export type OrganizationsPage = Page<Organization>;

/**
 * Slice 2 additions mirroring
 * server/internal/connections/driving/httpapi/dto.go and
 * server/internal/triggers/driving/httpapi/dto.go, read through the
 * AdminOrgScope console mount (/organizations/{orgId}/connections,
 * /organizations/{orgId}/trigger-instances).
 */
export type ConnectionStatus = "INITIATED" | "ACTIVE" | "EXPIRED" | "DISCONNECTED";

export interface ConnectionAccount {
  email: string;
  displayName: string;
}

export interface Connection {
  id: string;
  status: ConnectionStatus;
  providerSlug: string;
  userId: string;
  createdAt: string;
  account?: ConnectionAccount;
}

export type ConnectionsPage = Page<Connection>;

/** Disable's response shape (also reused for Reconnect's created connection
 * id/status pair before the redirectUrl is read separately). */
export interface ConnectionStatusResult {
  id: string;
  status: ConnectionStatus;
}

/** Reconnect's response: a fresh connect-page redirectUrl bound to the same,
 * immutable connection id (PD19) — surfaced to the operator so they can hand
 * it to the end user. */
export interface InitiatedConnection {
  id: string;
  status: ConnectionStatus;
  redirectUrl: string;
}

export type TriggerInstanceStatus = "ACTIVE" | "DISABLED";

export interface TriggerInstance {
  id: string;
  status: TriggerInstanceStatus;
  connectionId: string;
  triggerSlug: string;
  config: Record<string, unknown>;
  userId: string;
  createdAt: string;
}

export type TriggerInstancesPage = Page<TriggerInstance>;

/** Disable's and Enable's response shape. */
export interface TriggerInstanceStatusResult {
  id: string;
  status: TriggerInstanceStatus;
}

/** The PD5 error envelope every non-2xx /api/v1 response carries. */
export interface DomainErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

/**
 * Slice 3 additions mirroring
 * server/internal/logging/driving/httpapi/dto.go and
 * server/internal/delivery/driving/httpapi/dto.go, read through the
 * AdminOrgScope console mount (/organizations/{orgId}/logs,
 * /organizations/{orgId}/events).
 */
export type LogKind = "tool_execution" | "oauth_token_exchange" | "webhook_delivery" | "trigger_poll";

/** LogEntry is one already-redacted event-log row (AC1/AC6): requestBody
 * and responseBody are JSON strings whose sensitive fields were replaced
 * server-side with the literal "[REDACTED]" placeholder before the entry
 * was ever persisted — the console only ever renders what the server sent. */
export interface LogEntry {
  id: string;
  organizationId: string;
  userId?: string;
  connectionId?: string;
  toolSlug?: string;
  kind: LogKind;
  status: number;
  durationMs: number;
  requestBody: string;
  responseBody: string;
  rateLimited: boolean;
  eventId?: string;
  attempt?: number;
  createdAt: string;
}

export interface LogsPage {
  entries: LogEntry[];
  nextCursor?: string;
}

/** The delivery module's current Status enum (Phase 3, delivery/types.go).
 * RETRYING/DEAD are not returned by the backend today — an unrecognized
 * status still renders through StatusBadge's neutral fallback rather than
 * crashing (mirrors the trigger-instance ERROR forward-compat precedent). */
export type EventDeliveryStatus = "PENDING" | "DELIVERED" | "FAILED" | "NO_ENDPOINT";

export interface DeliveryEvent {
  id: string;
  type: string;
  createdAt: string;
  deliveryStatus: EventDeliveryStatus;
  attempts: number;
  lastAttemptAt?: string;
}

export type EventsPage = Page<DeliveryEvent>;

/**
 * Slice 4 additions mirroring
 * server/internal/organizations/driving/httpapi/users_dto.go (end-users) and
 * server/internal/access/driving/httpapi/dto.go (scope-restricted api keys),
 * read/written through the AdminOrgScope console mount
 * (/organizations/{orgId}/users, /organizations/{orgId}/api-keys).
 */
export interface EndUser {
  id: string;
  name: string;
  externalId: string;
  createdAt: string;
}

export type UsersPage = Page<EndUser>;

export type ApiKeyScope = "read-only" | "read-write";

/** ApiKeyListing is List's per-key response shape (AC3): prefix, scope,
 * created date, and rotation/revocation state — never a secret. */
export interface ApiKeyListing {
  id: string;
  prefix: string;
  scope: ApiKeyScope;
  createdAt: string;
  revokedAt?: string;
  rotatedAt?: string;
  overlapExpiresAt?: string;
}

/** IssuedApiKey is Issue's response: the only time the full secret ever
 * appears (AC4) — shown exactly once via SecretOnceModal. */
export interface IssuedApiKey {
  id: string;
  key: string;
  prefix: string;
  scope: ApiKeyScope;
  createdAt: string;
}

/** RotatedApiKey is Rotate's response: the new secret, shown exactly once
 * (AC6), plus when the outgoing secret's overlap window ends. */
export interface RotatedApiKey {
  id: string;
  key: string;
  prefix: string;
  overlapExpiresAt: string;
}

/**
 * Slice 5 additions mirroring
 * server/internal/organizations/driving/httpapi/governance_dto.go and
 * server/internal/catalog/driving/httpapi/dto.go's integrationVisibilityDTO,
 * read/written through the AdminOrgScope console mount
 * (/organizations/{orgId}/governance, /organizations/{orgId}/governance/catalog).
 */

/** Onboarding is Governance's nested featured-list shape (PD43): an ordered
 * subset of integration ids surfaced first during onboarding, capped at Cap. */
export interface Onboarding {
  featured: string[];
  cap: number;
}

/** Governance is one organization's allow-list, hidden set, and onboarding
 * configuration (PD42/PD43). allowList is null when the org inherits the
 * full installation catalog (the continuity-preserving default) — a non-null
 * array (even empty) restricts the org to exactly those integration ids. */
export interface Governance {
  allowList: string[] | null;
  hidden: string[];
  onboarding: Onboarding;
}

/** GovernanceUpdate is PUT .../governance's request body: it replaces the
 * org's entire governance record, mirroring Governance's own shape. */
export type GovernanceUpdate = Governance;

/** IntegrationVisibility is one row of GET .../governance/catalog's
 * unfiltered, operator-only view (AC1): every installation integration,
 * annotated with its effective visibility for the selected org. */
export type IntegrationEffectiveVisibility = "VISIBLE" | "HIDDEN" | "NOT_ALLOWED";

/** IntegrationSummary mirrors the backend's integrationSummaryDTO: the
 * shared installation-integration identity shape returned by POST
 * /api/v1/integrations, and the base every catalog/governance integration
 * row extends. It never carries the write-once clientSecret — that value is
 * accepted on create and never returned by any endpoint. */
export interface IntegrationSummary {
  id: string;
  providerSlug: string;
  name: string;
  logo: string;
  authScheme: string;
}

/** CreateIntegrationRequest mirrors the backend's createIntegrationRequest:
 * POST /api/v1/integrations' body — a provider-definition slug plus the
 * OAuth client credentials. clientSecret is write-once (sent on create,
 * never read back). */
export interface CreateIntegrationRequest {
  providerSlug: string;
  clientId: string;
  clientSecret: string;
}

export interface IntegrationVisibility extends IntegrationSummary {
  visibility: IntegrationEffectiveVisibility;
}

/**
 * Slice 7 additions mirroring
 * server/internal/organizations/driving/httpapi/retention_dto.go, read/
 * written through the AdminOrgScope console mount
 * (/organizations/{orgId}/retention).
 */

/** Retention is one organization's own log/event retention overrides
 * (PD44). logDays/eventDays are null when the org inherits the
 * installation's own BEECON_RETENTION_DAYS default (installationDefaultDays
 * names what that default currently is, so the console can render "inherit
 * default (N)" without hardcoding N); 0 means unlimited/disabled for that
 * entity kind — the purge worker never purges it, regardless of age. */
export interface Retention {
  logDays: number | null;
  eventDays: number | null;
  installationDefaultDays: number;
}

/** RetentionUpdate is PUT .../retention's request body: it replaces both
 * fields together (a whole-object PUT, mirroring GovernanceUpdate's own
 * replace convention scoped to just these two fields). */
export interface RetentionUpdate {
  logDays: number | null;
  eventDays: number | null;
}

/**
 * Slice 6 additions mirroring
 * server/internal/catalog/driving/httpapi/dto.go's provider-definition DTOs
 * (PD40): the installation-wide, un-governance-filtered CATALOG area
 * (Providers/Tools/Trigger Definitions) reads these through the new
 * admin-guarded GET /provider-definitions (+ /{slug}) endpoints — no orgId
 * in the path, and never filtered by any organization's governance (AC7).
 */
export interface ProviderDefinitionSummary {
  slug: string;
  name: string;
  logo: string;
  authScheme: string;
  formatVersion: number;
  toolCount: number;
  triggerCount: number;
}

export type ProviderDefinitionsPage = Page<ProviderDefinitionSummary>;

/** ProviderBundleTool/ProviderBundleTrigger are one tool/trigger as they
 * appear inside a provider definition's Bundle (AC2-AC4): the mono JSON/YAML
 * viewer renders the whole Bundle verbatim, while the Tools and Trigger
 * Definitions catalog pages read these same arrays back out, tagged with
 * their owning provider's identity, to build their cross-provider,
 * filterable-by-provider tables — no separate tools/trigger-definitions
 * catalog endpoint exists; a provider's Bundle already carries everything. */
export interface ProviderBundleTool {
  slug: string;
  name: string;
  description: string;
  deprecated: boolean;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
}

export interface ProviderBundleTrigger {
  slug: string;
  name: string;
  description: string;
  configSchema: Record<string, unknown>;
  payloadSchema: Record<string, unknown>;
  ingestion: string;
  pollIntervalSeconds: number;
}

/** ProviderDefinitionBundle is the JSON-serializable shape of Bundle
 * (AC2): reconstructs the finalized provider-definition file format (PD13)
 * field for field, so what an operator reads in the viewer matches the
 * provider definition file that produced it. */
export interface ProviderDefinitionBundle {
  formatVersion: number;
  slug: string;
  name: string;
  logo: string;
  authScheme: string;
  oauth: Record<string, unknown>;
  mapping: Record<string, unknown>;
  expectedParams: Array<Record<string, unknown>>;
  tools: ProviderBundleTool[];
  triggers: ProviderBundleTrigger[];
}

export interface ProviderDefinitionDetail {
  slug: string;
  name: string;
  formatVersion: number;
  bundle: ProviderDefinitionBundle;
}

/** CatalogTool/CatalogTriggerDefinition are one tool/trigger flattened out of
 * every loaded provider's Bundle, tagged with its owning provider's identity
 * (Slice 6, AC3) — the Tools and Trigger Definitions pages' row shape. */
export interface CatalogTool extends ProviderBundleTool {
  providerSlug: string;
  providerName: string;
}

export interface CatalogTriggerDefinition extends ProviderBundleTrigger {
  providerSlug: string;
  providerName: string;
}

/**
 * Slice 8 additions mirroring
 * server/internal/delivery/driving/httpapi/dto.go's multi-endpoint DTOs
 * (PD45): an org may register several webhook endpoints, each with its own
 * URL, optional event-type filter, status, and consecutive-failure count,
 * read/written through GET/POST /api/v1/webhook-endpoints (+ /{wepId}
 * variants) — reached through the AdminOrgScope console mount exactly like
 * every other org-scoped console area.
 */
export type WebhookEndpointStatus = "ENABLED" | "DISABLED" | "DISABLED_AUTO";

/** WebhookEndpoint is one item in GET /api/v1/webhook-endpoints' response
 * (AC1): never a secret, only its display prefix. eventTypes is null when
 * the endpoint matches every event type (PD45's continuity-preserving
 * default — the Phase 3 migration leaves the migrated single endpoint with
 * no filter). */
export interface WebhookEndpoint {
  id: string;
  url: string;
  eventTypes: string[] | null;
  status: WebhookEndpointStatus;
  consecutiveFailures: number;
  secretPrefix: string;
  createdAt: string;
}

/** CreatedWebhookEndpoint is POST .../webhook-endpoints' response: the
 * freshly minted secret, shown exactly once (AC1). */
export interface CreatedWebhookEndpoint {
  id: string;
  url: string;
  eventTypes: string[] | null;
  secret: string;
}

/** UpdatedWebhookEndpoint is PUT .../webhook-endpoints/{wepId}'s and
 * enable/disable's shared response shape: never a secret. */
export interface UpdatedWebhookEndpoint {
  id: string;
  url: string;
  eventTypes: string[] | null;
  status: WebhookEndpointStatus;
  consecutiveFailures: number;
  createdAt: string;
}

/** RotatedWebhookEndpointSecret is POST .../rotate-secret's response: the
 * new secret, shown exactly once (AC8). */
export interface RotatedWebhookEndpointSecret {
  secret: string;
}

/**
 * DashboardMetricsSummary mirrors
 * server/internal/metrics/summary_handler.go's summaryDTO (Slice 3,
 * architecture doc §3's "metrics read path" decision): a small typed JSON
 * read over the same Prometheus registry GET /metrics exposes in text
 * format, installation-wide (no org in the path).
 */
export interface DashboardMetricsSummary {
  connectionsByStatus: Record<string, number>;
  outbox: {
    pendingDepth: number;
    oldestPendingAgeSeconds: number;
  };
  deliveryOutcomes: Array<{ type: string; result: string; count: number }>;
}

/**
 * Slice 9 additions mirroring
 * server/internal/organizations/driving/httpapi/config_dto.go (PD46): a
 * versioned, secrets-free config document — governance, webhook endpoints
 * (URL + event-type filter only, never a secret), and retention — read
 * through GET .../config/export and written through POST .../config/import.
 * Deliberately has no field anywhere for an API-key/webhook secret,
 * credential, connection, user token, or provider definition.
 */

/** ConfigGovernance is ConfigDocument's governance section: the same
 * flattened shape (allowList/hidden/featured/featuredCap) the backend's own
 * ConfigGovernance carries — distinct from Governance's own nested
 * "onboarding" shape used by GET/PUT .../governance. */
export interface ConfigGovernance {
  allowList: string[] | null;
  hidden: string[];
  featured: string[];
  featuredCap: number;
}

/** ConfigEndpoint is one webhook endpoint in ConfigDocument's endpoints
 * section: URL and its event-type filter only — never a secret. */
export interface ConfigEndpoint {
  url: string;
  eventTypes: string[] | null;
}

/** ConfigRetention is ConfigDocument's retention section — the same
 * tri-state values Retention itself carries (null = inherit the
 * installation default; 0 = unlimited/disabled). */
export interface ConfigRetention {
  logRetentionDays: number | null;
  eventRetentionDays: number | null;
}

/** ConfigDocument is GET .../config/export's response and POST
 * .../config/import's request body: the versioned export/import document
 * itself. */
export interface ConfigDocument {
  schemaVersion: number;
  governance: ConfigGovernance;
  endpoints: ConfigEndpoint[];
  retention: ConfigRetention;
}

/** ConfigImportMode selects how an import reconciles the document against
 * an org's existing governance/endpoints/retention: "merge" (default)
 * upserts and leaves anything unmentioned untouched; "replace" makes the
 * organization match the document exactly, removing what it omits. */
export type ConfigImportMode = "merge" | "replace";

/** ConfigChange is one line of an import's dry-run plan or apply result:
 * which area (governance/retention/endpoint), which field or endpoint URL,
 * what action would happen/happened, and a human-readable summary. */
export interface ConfigChange {
  area: string;
  field: string;
  action: string;
  detail: string;
}

/** ConfigImportPlan is a dry-run import's response: the diff/plan it would
 * apply, plus any allow-listed/hidden/featured integration id that doesn't
 * exist in this installation, flagged rather than silently dropped. Nothing
 * is written for a dry-run. */
export interface ConfigImportPlan {
  plan: ConfigChange[];
  warnings: string[];
}

/** ConfigImportedSecret is one freshly minted webhook signing secret an
 * apply created for an endpoint the import introduced — shown exactly once,
 * since secrets are never part of an import document. */
export interface ConfigImportedSecret {
  wepId: string;
  secret: string;
}

/** ConfigImportApplyResult is a non-dry-run import's response: what was
 * applied, plus any freshly minted endpoint secrets. */
export interface ConfigImportApplyResult {
  applied: ConfigChange[];
  secrets: ConfigImportedSecret[];
}

/**
 * Phase 5 Slice 4 additions mirroring
 * server/internal/access/driving/httpapi/operator_dto.go: operator account
 * management (list/create/deactivate) and self-service password change,
 * read/written through GET/POST /api/v1/operators (+ /me/password,
 * /{opId}/deactivate) — installation-wide, never org-scoped, like the
 * operator's own session.
 */
export type OperatorStatus = "ACTIVE" | "DISABLED";

/** OperatorAccount is one row of GET /api/v1/operators' response (AC3):
 * email, status, and created date — never a password hash. */
export interface OperatorAccount {
  id: string;
  email: string;
  status: OperatorStatus;
  createdAt: string;
}

export type OperatorsPage = Page<OperatorAccount>;

/** CreatedOperator is POST /api/v1/operators' response (AC1): never the
 * password or its hash. */
export interface CreatedOperator {
  id: string;
  email: string;
}
