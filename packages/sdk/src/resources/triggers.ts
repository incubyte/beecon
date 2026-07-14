import type { HttpClient } from '../http.js';
import type {
  CreatedTriggerInstance,
  CreateTriggerInstanceInput,
  TriggerDefinition,
  TriggerDefinitionsFilter,
  TriggerDefinitionsPage,
  TriggerInstance,
  TriggerInstancesListFilter,
  TriggerInstancesPage,
  TriggerInstanceStatusResult,
  TriggersApi,
} from '../types.js';

export class TriggersResource implements TriggersApi {
  constructor(private readonly http: HttpClient) {}

  listDefinitions(filter: TriggerDefinitionsFilter = {}): Promise<TriggerDefinitionsPage> {
    return this.http.get<TriggerDefinitionsPage>('/api/v1/trigger-definitions', {
      providerSlug: filter.providerSlug,
      integrationId: filter.integrationId,
      cursor: filter.cursor,
      limit: filter.limit,
    });
  }

  getDefinition(slug: string): Promise<TriggerDefinition> {
    return this.http.get<TriggerDefinition>(
      `/api/v1/trigger-definitions/${encodeURIComponent(slug)}`,
    );
  }

  create(input: CreateTriggerInstanceInput): Promise<CreatedTriggerInstance> {
    return this.http.post<CreatedTriggerInstance>('/api/v1/trigger-instances', {
      connectionId: input.connectionId,
      triggerSlug: input.slug,
      config: input.config,
    });
  }

  list(filter: TriggerInstancesListFilter = {}): Promise<TriggerInstancesPage> {
    return this.http.get<TriggerInstancesPage>('/api/v1/trigger-instances', {
      connectionId: filter.connectionId,
      userId: filter.userId,
      cursor: filter.cursor,
      limit: filter.limit,
    });
  }

  get(triggerInstanceId: string): Promise<TriggerInstance> {
    return this.http.get<TriggerInstance>(
      `/api/v1/trigger-instances/${encodeURIComponent(triggerInstanceId)}`,
    );
  }

  enable(triggerInstanceId: string): Promise<TriggerInstanceStatusResult> {
    return this.http.post<TriggerInstanceStatusResult>(
      `/api/v1/trigger-instances/${encodeURIComponent(triggerInstanceId)}/enable`,
    );
  }

  disable(triggerInstanceId: string): Promise<TriggerInstanceStatusResult> {
    return this.http.post<TriggerInstanceStatusResult>(
      `/api/v1/trigger-instances/${encodeURIComponent(triggerInstanceId)}/disable`,
    );
  }

  delete(triggerInstanceId: string): Promise<void> {
    return this.http.delete<void>(
      `/api/v1/trigger-instances/${encodeURIComponent(triggerInstanceId)}`,
    );
  }
}
