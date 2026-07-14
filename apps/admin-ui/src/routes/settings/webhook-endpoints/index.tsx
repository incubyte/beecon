import { createFileRoute } from "@tanstack/react-router";

import { WebhookEndpointsPage } from "@/features/webhook-endpoints/WebhookEndpointsPage";

export const Route = createFileRoute("/settings/webhook-endpoints/")({
  component: WebhookEndpointsPage,
});
