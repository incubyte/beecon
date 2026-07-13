import type { HttpClient } from '../http.js';
import type { ExecuteToolInput, Tool, ToolExecutionResult, ToolsApi, ToolsFilter, ToolsPage } from '../types.js';

export class ToolsResource implements ToolsApi {
  constructor(private readonly http: HttpClient) {}

  list(filter: ToolsFilter = {}): Promise<ToolsPage> {
    return this.http.get<ToolsPage>('/api/v1/tools', {
      integrationId: filter.integrationId,
      providerSlug: filter.providerSlug,
      includeDeprecated: filter.includeDeprecated,
      cursor: filter.cursor,
      limit: filter.limit,
    });
  }

  get(slug: string): Promise<Tool> {
    return this.http.get<Tool>(`/api/v1/tools/${encodeURIComponent(slug)}`);
  }

  execute<TData = unknown>(
    slug: string,
    input: ExecuteToolInput,
  ): Promise<ToolExecutionResult<TData>> {
    return this.http.post<ToolExecutionResult<TData>>(
      `/api/v1/tools/${encodeURIComponent(slug)}/execute`,
      input,
    );
  }
}
