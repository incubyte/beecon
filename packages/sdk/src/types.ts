// Wire types mirror the Go DTOs field-for-field (see
// server/internal/*/driving/httpapi/dto.go) so the SDK never guesses at the
// API shape.

export interface User {
  id: string;
  name: string;
  externalId: string;
  createdAt: string;
}

export interface CreateUserInput {
  name: string;
  externalId?: string;
}

export interface UsersApi {
  create(input: CreateUserInput): Promise<User>;
}

export interface Integration {
  id: string;
  providerSlug: string;
  name: string;
  logo: string;
  authScheme: string;
}

export interface IntegrationsApi {
  list(): Promise<Integration[]>;
  getExpectedParams(integrationId: string): Promise<ExpectedParams>;
}

// Phase 2 (PD19) adds EXPIRED (refresh failed) and DISCONNECTED (disabled)
// to Phase 1's INITIATED/ACTIVE.
export type ConnectionStatus = 'INITIATED' | 'ACTIVE' | 'EXPIRED' | 'DISCONNECTED';

export interface InitiateConnectionInput {
  userId: string;
  integrationId: string;
  redirectUri: string;
}

export interface InitiatedConnection {
  id: string;
  status: ConnectionStatus;
  redirectUrl: string;
}

export interface ConnectionAccount {
  email: string;
  displayName: string;
}

// Connection never carries tokens — the vault-encrypted access/refresh
// tokens are not part of the wire format at all (AC5).
export interface Connection {
  id: string;
  status: ConnectionStatus;
  providerSlug: string;
  userId: string;
  createdAt: string;
  account?: ConnectionAccount;
}

// ConnectionsListFilter mirrors GET /api/v1/connections' query params
// (Slice 4, PD15a): cursor-paginated, optionally narrowed to one user.
export interface ConnectionsListFilter {
  userId?: string;
  cursor?: string;
  limit?: number;
}

export interface ConnectionsPage {
  items: Connection[];
  nextCursor?: string;
}

// ConnectionStatusResult is Disable's response: the connection's id and its
// new status (never the connection's other fields).
export interface ConnectionStatusResult {
  id: string;
  status: ConnectionStatus;
}

// ReconnectInput is Reconnect's body (PD19): the redirectUri this reconnect
// attempt's connect page forwards to. Reconnect always keeps the same
// connection id, returning a fresh redirectUrl.
export interface ReconnectInput {
  redirectUri: string;
}

export interface ConnectionsApi {
  initiate(input: InitiateConnectionInput): Promise<InitiatedConnection>;
  get(connectionId: string): Promise<Connection>;
  list(filter?: ConnectionsListFilter): Promise<ConnectionsPage>;
  disable(connectionId: string): Promise<ConnectionStatusResult>;
  delete(connectionId: string): Promise<void>;
  reconnect(connectionId: string, input: ReconnectInput): Promise<InitiatedConnection>;
}

// ToolProvider is the provider identity nested inside a Tool (a tool
// addressed by slug alone still needs to know which provider it belongs to).
export interface ToolProvider {
  slug: string;
  name: string;
  logo: string;
}

// Tool is one catalog entry as GET /api/v1/tools and GET /api/v1/tools/{slug}
// return it: accurate input and output JSON Schemas (PD13), and whether the
// tool is deprecated.
export interface Tool {
  slug: string;
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  deprecated: boolean;
  provider: ToolProvider;
}

// ToolsFilter mirrors GET /api/v1/tools' query params (PD15a): scope by
// integration or provider slug, optionally including deprecated tools,
// cursor-paginated.
export interface ToolsFilter {
  integrationId?: string;
  providerSlug?: string;
  includeDeprecated?: boolean;
  cursor?: string;
  limit?: number;
}

export interface ToolsPage {
  items: Tool[];
  nextCursor?: string;
}

export interface ExecuteToolInput {
  userId: string;
  connectionId: string;
  arguments: Record<string, unknown>;
}

export interface ToolExecutionError {
  code: string;
  message: string;
}

// Tool-level outcomes are always HTTP 200 (PD6): a failed tool call is a
// value, not a thrown error. Only platform-level HTTP failures throw
// BeeconApiError (or its RateLimitedError subclass, PD21). nextCursor is
// PD15b's pagination carve-out for tools whose mapping declares pagination.
export interface ToolExecutionResult<TData = unknown> {
  successful: boolean;
  error: ToolExecutionError | null;
  data: TData | null;
  nextCursor?: string;
}

export interface ToolsApi {
  list(filter?: ToolsFilter): Promise<ToolsPage>;
  get(slug: string): Promise<Tool>;
  execute<TData = unknown>(
    slug: string,
    input: ExecuteToolInput,
  ): Promise<ToolExecutionResult<TData>>;
}

// ExpectedParamField is one field of GET
// /api/v1/integrations/{integrationId}/expected-params' response (Slice 3):
// never a value — only the field's own shape.
export interface ExpectedParamField {
  name: string;
  displayName: string;
  description: string;
  required: boolean;
  secret: boolean;
}

export interface ExpectedParams {
  providerName: string;
  fields: ExpectedParamField[];
}

// SigningSecretConfig is the user-token signing secret configured on the
// Beecon constructor (PD20): id is the usk_-prefixed signing-secret id
// (the JWT's "kid" header), secret is the raw HS256 signing key. Never
// stored on an enumerable property — see UserTokensResource.
export interface SigningSecretConfig {
  id: string;
  secret: string;
}

export interface CreateUserTokenInput {
  userId: string;
  /**
   * Seconds until expiry; defaults to 2 hours (PD20). Must not exceed 24
   * hours (86400s) — the server rejects a longer-lived token anyway (PD38a),
   * so create throws UserTokenExpiryTooLongError rather than minting one.
   */
  expiresIn?: number;
}

export interface UserToken {
  token: string;
  expiresAt: string;
}

// UserTokensApi mints a user-scoped browser token entirely locally (PD20) —
// create makes no network call, so it is synchronous rather than returning
// a Promise like every other resource method in this SDK.
export interface UserTokensApi {
  create(input: CreateUserTokenInput): UserToken;
}

export interface UploadFileInput {
  fileName: string;
  mimeType?: string;
  content: Blob | Uint8Array | ArrayBuffer;
}

export interface UploadedFile {
  id: string;
  name: string;
  mimeType: string;
  size: number;
  downloadUrl: string;
}

export interface FilesApi {
  upload(input: UploadFileInput): Promise<UploadedFile>;
}

export interface LogEntry {
  id: string;
  organizationId: string;
  userId?: string;
  connectionId?: string;
  toolSlug?: string;
  kind: string;
  status: number;
  durationMs: number;
  requestBody: string;
  responseBody: string;
  createdAt: string;
}

export interface LogsFilter {
  connectionId?: string;
  userId?: string;
  toolSlug?: string;
  /** RFC3339 timestamp, e.g. new Date().toISOString() */
  from?: string;
  /** RFC3339 timestamp, e.g. new Date().toISOString() */
  to?: string;
  cursor?: string;
  limit?: number;
}

export interface LogsPage {
  entries: LogEntry[];
  nextCursor?: string;
}

export interface LogsApi {
  list(filters?: LogsFilter): Promise<LogsPage>;
}

// TriggerDefinitionProvider is the provider identity nested inside a
// TriggerDefinition (PD35) — mirrors ToolProvider: a trigger addressed by
// slug alone (PD14) still needs to know which provider it belongs to.
export interface TriggerDefinitionProvider {
  slug: string;
  name: string;
  logo: string;
}

// TriggerDefinition is one catalog entry as GET /api/v1/trigger-definitions
// and GET /api/v1/trigger-definitions/{slug} return it: config and payload
// JSON Schemas (PD32), and the ingestion mode (poll-only in Phase 3, PD28).
export interface TriggerDefinition {
  slug: string;
  name: string;
  description: string;
  configSchema: Record<string, unknown>;
  payloadSchema: Record<string, unknown>;
  ingestion: 'poll';
  provider: TriggerDefinitionProvider;
}

// TriggerDefinitionsFilter mirrors GET /api/v1/trigger-definitions' query
// params: scope by provider slug or integration id, cursor-paginated.
export interface TriggerDefinitionsFilter {
  providerSlug?: string;
  integrationId?: string;
  cursor?: string;
  limit?: number;
}

export interface TriggerDefinitionsPage {
  items: TriggerDefinition[];
  nextCursor?: string;
}

// TriggerInstanceStatus mirrors PD33's lifecycle: born ACTIVE, DISABLED
// stops firing without losing poll state, re-enabling resumes.
export type TriggerInstanceStatus = 'ACTIVE' | 'DISABLED';

// CreateTriggerInstanceInput is POST /api/v1/trigger-instances' body (API
// Shape). `slug` names the trigger definition; the wire field is
// `triggerSlug` — TriggerInstance below uses that same wire name once the
// instance exists, since GET/list responses echo the server's own DTO.
export interface CreateTriggerInstanceInput {
  connectionId: string;
  slug: string;
  config: Record<string, unknown>;
}

export interface CreatedTriggerInstance {
  id: string;
  status: TriggerInstanceStatus;
}

// TriggerInstance is Get's and List's per-item response: status, trigger
// slug, connection, config, and owning user (PD33).
export interface TriggerInstance {
  id: string;
  status: TriggerInstanceStatus;
  connectionId: string;
  triggerSlug: string;
  config: Record<string, unknown>;
  userId: string;
  createdAt: string;
}

// TriggerInstancesListFilter mirrors GET /api/v1/trigger-instances' query
// params: scope by connection or user, cursor-paginated.
export interface TriggerInstancesListFilter {
  connectionId?: string;
  userId?: string;
  cursor?: string;
  limit?: number;
}

export interface TriggerInstancesPage {
  items: TriggerInstance[];
  nextCursor?: string;
}

// TriggerInstanceStatusResult is Disable's and Enable's response: the
// instance's id and its new status.
export interface TriggerInstanceStatusResult {
  id: string;
  status: TriggerInstanceStatus;
}

export interface TriggersApi {
  listDefinitions(filter?: TriggerDefinitionsFilter): Promise<TriggerDefinitionsPage>;
  getDefinition(slug: string): Promise<TriggerDefinition>;
  create(input: CreateTriggerInstanceInput): Promise<CreatedTriggerInstance>;
  list(filter?: TriggerInstancesListFilter): Promise<TriggerInstancesPage>;
  get(triggerInstanceId: string): Promise<TriggerInstance>;
  enable(triggerInstanceId: string): Promise<TriggerInstanceStatusResult>;
  disable(triggerInstanceId: string): Promise<TriggerInstanceStatusResult>;
  delete(triggerInstanceId: string): Promise<void>;
}

export interface SetWebhookEndpointInput {
  url: string;
}

// WebhookEndpointCreated is SetEndpoint's response (API Shape, PD31): secret
// is present only on first creation — a later URL-only update leaves it
// undefined (mirrors the server's `omitempty`). It is returned exactly once;
// nothing in this SDK stores it (AC: never logged, never serialized).
export interface WebhookEndpointCreated {
  id: string;
  url: string;
  secret?: string;
  createdAt: string;
}

// WebhookEndpoint is GetEndpoint's response: URL, secret prefix, and
// creation date — never the full secret.
export interface WebhookEndpoint {
  id: string;
  url: string;
  secretPrefix: string;
  createdAt: string;
}

// RotateSecretInput is RotateSecret's body (PD31): overlapHours defaults to
// the server's own default (24h) when omitted.
export interface RotateSecretInput {
  overlapHours?: number;
}

// RotatedSecret is RotateSecret's response: the new secret, returned
// exactly once.
export interface RotatedSecret {
  secret: string;
}

export interface WebhookEndpointApi {
  set(input: SetWebhookEndpointInput): Promise<WebhookEndpointCreated>;
  get(): Promise<WebhookEndpoint>;
  rotateSecret(input?: RotateSecretInput): Promise<RotatedSecret>;
  sendTest(): Promise<void>;
}

// EventDeliveryStatus mirrors the outbox's delivery lifecycle (PD30):
// PENDING awaiting/retrying, DELIVERED on a 2xx, FAILED after exhausting the
// retry schedule, NO_ENDPOINT when the organization has none configured yet.
export type EventDeliveryStatus = 'PENDING' | 'DELIVERED' | 'FAILED' | 'NO_ENDPOINT';

// OutboxEvent is one item in GET /api/v1/events' response (API Shape) —
// metadata only, never the event body (fetch a redelivery to see it land at
// your own endpoint).
export interface OutboxEvent {
  id: string;
  type: string;
  createdAt: string;
  deliveryStatus: EventDeliveryStatus;
  attempts: number;
  lastAttemptAt?: string;
}

export interface EventsFilter {
  type?: string;
  deliveryStatus?: EventDeliveryStatus;
  cursor?: string;
  limit?: number;
}

export interface EventsPage {
  items: OutboxEvent[];
  nextCursor?: string;
}

export interface EventsApi {
  list(filters?: EventsFilter): Promise<EventsPage>;
  redeliver(eventId: string): Promise<void>;
}

// --- PD32 event envelope: the typed union webhooks.verify returns --------

export interface TriggerEventData {
  triggerInstanceId: string;
  triggerSlug: string;
  connectionId: string;
  userId: string;
  payload: Record<string, unknown>;
}

export interface ConnectionExpiredEventData {
  connectionId: string;
  userId: string;
  integrationId: string;
  providerSlug: string;
  reason: string;
}

// WebhookTestEventData is always empty — webhook.test proves the channel
// works before anything real depends on it (PD32).
export type WebhookTestEventData = Record<string, never>;

export interface TriggerEvent {
  id: string;
  type: 'trigger.event';
  createdAt: string;
  data: TriggerEventData;
}

export interface ConnectionExpiredEvent {
  id: string;
  type: 'connection.expired';
  createdAt: string;
  data: ConnectionExpiredEventData;
}

export interface WebhookTestEvent {
  id: string;
  type: 'webhook.test';
  createdAt: string;
  data: WebhookTestEventData;
}

// WebhookEvent is the typed union webhooks.verify returns once a delivery's
// signature and timestamp both check out (PD32). The `id` is the
// idempotency key: identical across retries and manual redeliveries —
// consumers deduplicate on it.
export type WebhookEvent = TriggerEvent | ConnectionExpiredEvent | WebhookTestEvent;

// VerifyWebhookHeaders accepts either a plain header map (Express/Node
// style — Record, case-sensitivity of keys not assumed) or a WHATWG Headers
// instance (Next.js Request-style), so webhooks.verify drops into either
// framework's handler unchanged.
export type VerifyWebhookHeaders = Headers | Record<string, string | string[] | undefined>;

export interface VerifyWebhookInput {
  /** The exact raw request body bytes/string Beecon signed — do not re-serialize a parsed object. */
  payload: string;
  headers: VerifyWebhookHeaders;
  /** A single active endpoint secret. Mutually exclusive with `secrets`. */
  secret?: string;
  /** Every secret the consumer currently holds (rotation overlap, PD31) — verification succeeds if any one matches. */
  secrets?: string[];
  /** Injectable clock for tests; defaults to `new Date()`. */
  now?: Date;
}

export interface WebhooksApi {
  verify(input: VerifyWebhookInput): WebhookEvent;
}

// BeeconClient is the SDK's full public surface. Consumers type against this
// interface (not the Beecon class) so a test double built with vi.fn() —
// e.g. `{ users: { create: vi.fn() }, ... } satisfies BeeconClient` — is a
// drop-in replacement for the real client.
export interface BeeconClient {
  readonly users: UsersApi;
  readonly integrations: IntegrationsApi;
  readonly connections: ConnectionsApi;
  readonly tools: ToolsApi;
  readonly logs: LogsApi;
  readonly userTokens: UserTokensApi;
  readonly files: FilesApi;
  readonly triggers: TriggersApi;
  readonly webhookEndpoint: WebhookEndpointApi;
  readonly events: EventsApi;
  readonly webhooks: WebhooksApi;
}
