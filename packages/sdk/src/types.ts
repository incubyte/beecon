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
  /** Seconds until expiry; defaults to 2 hours (PD20). */
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
}
