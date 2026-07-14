export { Beecon } from './client.js';
export type { BeeconConfig } from './client.js';

export {
  BeeconApiError,
  MissingSigningSecretError,
  RateLimitedError,
  UserTokenExpiryTooLongError,
  WebhookVerificationError,
} from './errors.js';
export type { ApiErrorBody, WebhookVerificationReason } from './errors.js';

export type { FetchLike } from './http.js';

// Standalone webhooks.verify — verifying a delivery is a pure, local
// operation (no network, no API key), so it is usable in a lightweight
// handler without constructing a full Beecon client:
//   import { webhooks } from '@beecon/sdk';
//   const event = webhooks.verify({ payload, headers, secret });
export * as webhooks from './webhooks.js';

export type {
  BeeconClient,
  Connection,
  ConnectionAccount,
  ConnectionExpiredEvent,
  ConnectionExpiredEventData,
  ConnectionsApi,
  ConnectionsListFilter,
  ConnectionsPage,
  ConnectionStatus,
  ConnectionStatusResult,
  CreatedTriggerInstance,
  CreateTriggerInstanceInput,
  CreateUserInput,
  CreateUserTokenInput,
  EventDeliveryStatus,
  EventsApi,
  EventsFilter,
  EventsPage,
  ExecuteToolInput,
  ExpectedParamField,
  ExpectedParams,
  FilesApi,
  Integration,
  IntegrationsApi,
  InitiateConnectionInput,
  InitiatedConnection,
  LogEntry,
  LogsApi,
  LogsFilter,
  LogsPage,
  OutboxEvent,
  ReconnectInput,
  RotatedSecret,
  RotateSecretInput,
  SetWebhookEndpointInput,
  SigningSecretConfig,
  Tool,
  ToolExecutionError,
  ToolExecutionResult,
  ToolProvider,
  ToolsApi,
  ToolsFilter,
  ToolsPage,
  TriggerDefinition,
  TriggerDefinitionProvider,
  TriggerDefinitionsFilter,
  TriggerDefinitionsPage,
  TriggerEvent,
  TriggerEventData,
  TriggerInstance,
  TriggerInstancesListFilter,
  TriggerInstancesPage,
  TriggerInstanceStatus,
  TriggerInstanceStatusResult,
  TriggersApi,
  UploadedFile,
  UploadFileInput,
  User,
  UserToken,
  UserTokensApi,
  UsersApi,
  VerifyWebhookHeaders,
  VerifyWebhookInput,
  WebhookEndpoint,
  WebhookEndpointApi,
  WebhookEndpointCreated,
  WebhookEvent,
  WebhooksApi,
  WebhookTestEvent,
  WebhookTestEventData,
} from './types.js';
