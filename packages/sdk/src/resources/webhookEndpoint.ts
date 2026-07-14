import type { HttpClient } from '../http.js';
import type {
  RotatedSecret,
  RotateSecretInput,
  SetWebhookEndpointInput,
  WebhookEndpoint,
  WebhookEndpointApi,
  WebhookEndpointCreated,
} from '../types.js';

// WebhookEndpointResource holds no state beyond the shared HttpClient — the
// whsec_ secret set/rotateSecret return is never assigned to a field of this
// class, only handed back once in the resolved promise, so nothing this SDK
// owns can leak it via JSON.stringify, util.inspect, or a thrown error
// (parity with the API-key and signing-secret guarantee, AC).
export class WebhookEndpointResource implements WebhookEndpointApi {
  constructor(private readonly http: HttpClient) {}

  set(input: SetWebhookEndpointInput): Promise<WebhookEndpointCreated> {
    return this.http.put<WebhookEndpointCreated>('/api/v1/webhook-endpoint', { url: input.url });
  }

  get(): Promise<WebhookEndpoint> {
    return this.http.get<WebhookEndpoint>('/api/v1/webhook-endpoint');
  }

  rotateSecret(input: RotateSecretInput = {}): Promise<RotatedSecret> {
    return this.http.post<RotatedSecret>('/api/v1/webhook-endpoint/rotate-secret', {
      overlapHours: input.overlapHours,
    });
  }

  sendTest(): Promise<void> {
    return this.http.post<void>('/api/v1/webhook-endpoint/test');
  }
}
