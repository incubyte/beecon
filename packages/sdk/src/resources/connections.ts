import type { HttpClient } from '../http.js';
import type {
  Connection,
  ConnectionsApi,
  ConnectionsListFilter,
  ConnectionsPage,
  ConnectionStatusResult,
  InitiateConnectionInput,
  InitiatedConnection,
  ReconnectInput,
} from '../types.js';

export class ConnectionsResource implements ConnectionsApi {
  constructor(private readonly http: HttpClient) {}

  initiate(input: InitiateConnectionInput): Promise<InitiatedConnection> {
    return this.http.post<InitiatedConnection>('/api/v1/connections/initiate', input);
  }

  get(connectionId: string): Promise<Connection> {
    return this.http.get<Connection>(`/api/v1/connections/${encodeURIComponent(connectionId)}`);
  }

  list(filter: ConnectionsListFilter = {}): Promise<ConnectionsPage> {
    return this.http.get<ConnectionsPage>('/api/v1/connections', {
      userId: filter.userId,
      cursor: filter.cursor,
      limit: filter.limit,
    });
  }

  disable(connectionId: string): Promise<ConnectionStatusResult> {
    return this.http.post<ConnectionStatusResult>(
      `/api/v1/connections/${encodeURIComponent(connectionId)}/disable`,
    );
  }

  delete(connectionId: string): Promise<void> {
    return this.http.delete<void>(`/api/v1/connections/${encodeURIComponent(connectionId)}`);
  }

  reconnect(connectionId: string, input: ReconnectInput): Promise<InitiatedConnection> {
    return this.http.post<InitiatedConnection>(
      `/api/v1/connections/${encodeURIComponent(connectionId)}/reconnect`,
      input,
    );
  }
}
