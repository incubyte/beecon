import type { HttpClient } from '../http.js';
import type { LogsApi, LogsFilter, LogsPage } from '../types.js';

export class LogsResource implements LogsApi {
  constructor(private readonly http: HttpClient) {}

  list(filters: LogsFilter = {}): Promise<LogsPage> {
    return this.http.get<LogsPage>('/api/v1/logs', {
      connectionId: filters.connectionId,
      userId: filters.userId,
      toolSlug: filters.toolSlug,
      from: filters.from,
      to: filters.to,
      cursor: filters.cursor,
      limit: filters.limit,
    });
  }
}
