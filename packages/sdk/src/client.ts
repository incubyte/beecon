import { HttpClient, type FetchLike } from './http.js';
import { ConnectionsResource } from './resources/connections.js';
import { EventsResource } from './resources/events.js';
import { FilesResource } from './resources/files.js';
import { IntegrationsResource } from './resources/integrations.js';
import { LogsResource } from './resources/logs.js';
import { ToolsResource } from './resources/tools.js';
import { TriggersResource } from './resources/triggers.js';
import { UserTokensResource } from './resources/userTokens.js';
import { UsersResource } from './resources/users.js';
import { WebhookEndpointResource } from './resources/webhookEndpoint.js';
import { verify } from './webhooks.js';
import type {
  BeeconClient,
  ConnectionsApi,
  EventsApi,
  FilesApi,
  IntegrationsApi,
  LogsApi,
  SigningSecretConfig,
  ToolsApi,
  TriggersApi,
  UsersApi,
  UserTokensApi,
  WebhookEndpointApi,
  WebhooksApi,
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
  readonly triggers: TriggersApi;
  readonly webhookEndpoint: WebhookEndpointApi;
  readonly events: EventsApi;
  readonly webhooks: WebhooksApi;

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
    this.triggers = new TriggersResource(http);
    this.webhookEndpoint = new WebhookEndpointResource(http);
    this.events = new EventsResource(http);
    // webhooks.verify is a pure function (no network, no client state) —
    // exposed here purely for the `beecon.webhooks.verify(...)` call shape
    // (also available standalone as the top-level `webhooks` export).
    this.webhooks = { verify };
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
