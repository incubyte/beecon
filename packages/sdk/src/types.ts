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
}

export type ConnectionStatus = 'INITIATED' | 'ACTIVE';

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

export interface ConnectionsApi {
  initiate(input: InitiateConnectionInput): Promise<InitiatedConnection>;
  get(connectionId: string): Promise<Connection>;
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
// BeeconApiError.
export interface ToolExecutionResult<TData = unknown> {
  successful: boolean;
  error: ToolExecutionError | null;
  data: TData | null;
}

export interface ToolsApi {
  execute<TData = unknown>(
    slug: string,
    input: ExecuteToolInput,
  ): Promise<ToolExecutionResult<TData>>;
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
}
