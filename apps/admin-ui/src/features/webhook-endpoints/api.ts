import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type {
  CreatedWebhookEndpoint,
  RotatedWebhookEndpointSecret,
  UpdatedWebhookEndpoint,
  WebhookEndpoint,
} from "@/lib/api-types";

interface WebhookEndpointsResponse {
  items: WebhookEndpoint[];
}

/** useWebhookEndpoints lists the selected org's webhook endpoints (Slice 8,
 * AC1). Like api-keys' own List, this is not cursor-paginated — the
 * per-org cap keeps the list small — so a plain useQuery over the flat
 * items array. */
export function useWebhookEndpoints(orgId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.webhookEndpoints.list(orgId ?? ""),
    queryFn: async () => {
      const page = await apiClient.get<WebhookEndpointsResponse>(`/organizations/${orgId}/webhook-endpoints`);
      return page.items;
    },
    enabled: Boolean(orgId),
  });
}

export interface CreateWebhookEndpointInput {
  url: string;
  eventTypes: string[] | null;
}

/** useCreateWebhookEndpoint registers a new endpoint (AC1): rejected with
 * a validation error naming the configured cap once the org already holds
 * the maximum (AC2) — surfaced by the caller via ApiError. The response's
 * full secret must be shown to the operator exactly once (SecretOnceModal)
 * and never persisted client-side beyond that. */
export function useCreateWebhookEndpoint(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateWebhookEndpointInput) =>
      apiClient.post<CreatedWebhookEndpoint>(`/organizations/${orgId}/webhook-endpoints`, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}

export interface UpdateWebhookEndpointInput {
  wepId: string;
  url: string;
  eventTypes: string[] | null;
}

/** useUpdateWebhookEndpoint replaces one endpoint's URL and event-type
 * filter (Slice 8: editing the filter). */
export function useUpdateWebhookEndpoint(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ wepId, ...body }: UpdateWebhookEndpointInput) =>
      apiClient.put<UpdatedWebhookEndpoint>(`/organizations/${orgId}/webhook-endpoints/${wepId}`, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}

/** useRotateWebhookEndpointSecret mints a fresh secret for one endpoint
 * (AC8): the new secret is shown exactly once; the outgoing secret keeps
 * authenticating until its overlap window ends (PD27/PD23). */
export function useRotateWebhookEndpointSecret(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (wepId: string) =>
      apiClient.post<RotatedWebhookEndpointSecret>(`/organizations/${orgId}/webhook-endpoints/${wepId}/rotate-secret`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}

/** useEnableWebhookEndpoint resumes fan-out and resets the
 * consecutive-failure counter (AC6) — the same action for an endpoint an
 * operator disabled themselves or one auto-disable bookkeeping quarantined. */
export function useEnableWebhookEndpoint(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (wepId: string) => apiClient.post<UpdatedWebhookEndpoint>(`/organizations/${orgId}/webhook-endpoints/${wepId}/enable`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}

/** useDisableWebhookEndpoint turns an endpoint off by operator request. */
export function useDisableWebhookEndpoint(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (wepId: string) => apiClient.post<UpdatedWebhookEndpoint>(`/organizations/${orgId}/webhook-endpoints/${wepId}/disable`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}

/** useDeleteWebhookEndpoint permanently removes an endpoint (AC8). */
export function useDeleteWebhookEndpoint(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (wepId: string) => apiClient.delete<void>(`/organizations/${orgId}/webhook-endpoints/${wepId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}
