import { HttpClient, type FetchLike } from './http.js';
import { ConnectionsResource } from './resources/connections.js';
import { FilesResource } from './resources/files.js';
import { IntegrationsResource } from './resources/integrations.js';
import { LogsResource } from './resources/logs.js';
import { ToolsResource } from './resources/tools.js';
import { UserTokensResource } from './resources/userTokens.js';
import { UsersResource } from './resources/users.js';
import type {
  BeeconClient,
  ConnectionsApi,
  FilesApi,
  IntegrationsApi,
  LogsApi,
  SigningSecretConfig,
  ToolsApi,
  UsersApi,
  UserTokensApi,
} from './types.js';

export interface BeeconConfig {
  apiKey: string;
  baseUrl: string;
  /** Injectable fetch implementation; defaults to globalThis.fetch (Node 18+). */
  fetch?: FetchLike;
  /** User-token signing secret (PD20); required only for userTokens.create. */
  signingSecret?: SigningSecretConfig;
}

const inspectSymbol = Symbol.for('nodejs.util.inspect.custom');

// Beecon is the concrete client. Consumers should type against the
// BeeconClient interface so a vi.fn()-built double is a drop-in replacement;
// the class itself is only ever constructed at the composition root.
export class Beecon implements BeeconClient {
  readonly users: UsersApi;
  readonly integrations: IntegrationsApi;
  readonly connections: ConnectionsApi;
  readonly tools: ToolsApi;
  readonly logs: LogsApi;
  readonly userTokens: UserTokensApi;
  readonly files: FilesApi;

  readonly #baseUrl: string;

  constructor(config: BeeconConfig) {
    const http = new HttpClient({
      apiKey: config.apiKey,
      baseUrl: config.baseUrl,
      fetchImpl: config.fetch,
    });
    this.#baseUrl = config.baseUrl;
    this.users = new UsersResource(http);
    this.integrations = new IntegrationsResource(http);
    this.connections = new ConnectionsResource(http);
    this.tools = new ToolsResource(http);
    this.logs = new LogsResource(http);
    this.userTokens = new UserTokensResource(config.signingSecret);
    this.files = new FilesResource(http);
  }

  // AC9: the API key never entered a property of this instance, so
  // JSON.stringify(client) and console.log(client) both surface only the
  // baseUrl.
  toJSON(): { baseUrl: string } {
    return { baseUrl: this.#baseUrl };
  }

  [inspectSymbol](): string {
    return `Beecon { baseUrl: ${JSON.stringify(this.#baseUrl)} }`;
  }
}
