import type { HttpClient } from '../http.js';
import type { EventsApi, EventsFilter, EventsPage } from '../types.js';

export class EventsResource implements EventsApi {
  constructor(private readonly http: HttpClient) {}

  list(filters: EventsFilter = {}): Promise<EventsPage> {
    return this.http.get<EventsPage>('/api/v1/events', {
      type: filters.type,
      deliveryStatus: filters.deliveryStatus,
      cursor: filters.cursor,
      limit: filters.limit,
    });
  }

  redeliver(eventId: string): Promise<void> {
    return this.http.post<void>(`/api/v1/events/${encodeURIComponent(eventId)}/redeliver`);
  }
}
