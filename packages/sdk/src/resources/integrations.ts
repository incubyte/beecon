import type { HttpClient } from '../http.js';
import type { Integration, IntegrationsApi } from '../types.js';

export class IntegrationsResource implements IntegrationsApi {
  constructor(private readonly http: HttpClient) {}

  list(): Promise<Integration[]> {
    return this.http.get<Integration[]>('/api/v1/integrations');
  }
}
