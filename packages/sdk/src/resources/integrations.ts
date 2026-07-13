import type { HttpClient } from '../http.js';
import type { ExpectedParams, Integration, IntegrationsApi } from '../types.js';

export class IntegrationsResource implements IntegrationsApi {
  constructor(private readonly http: HttpClient) {}

  list(): Promise<Integration[]> {
    return this.http.get<Integration[]>('/api/v1/integrations');
  }

  getExpectedParams(integrationId: string): Promise<ExpectedParams> {
    return this.http.get<ExpectedParams>(
      `/api/v1/integrations/${encodeURIComponent(integrationId)}/expected-params`,
    );
  }
}
