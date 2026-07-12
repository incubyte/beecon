import type { HttpClient } from '../http.js';
import type { ExecuteToolInput, ToolExecutionResult, ToolsApi } from '../types.js';

export class ToolsResource implements ToolsApi {
  constructor(private readonly http: HttpClient) {}

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
