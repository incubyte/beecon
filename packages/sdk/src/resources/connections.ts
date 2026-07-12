import type { HttpClient } from '../http.js';
import type {
  Connection,
  ConnectionsApi,
  InitiateConnectionInput,
  InitiatedConnection,
} from '../types.js';

export class ConnectionsResource implements ConnectionsApi {
  constructor(private readonly http: HttpClient) {}

  initiate(input: InitiateConnectionInput): Promise<InitiatedConnection> {
    return this.http.post<InitiatedConnection>('/api/v1/connections/initiate', input);
  }

  get(connectionId: string): Promise<Connection> {
    return this.http.get<Connection>(`/api/v1/connections/${encodeURIComponent(connectionId)}`);
  }
}
