import { createFileRoute } from "@tanstack/react-router";

import { TriggerInstanceDetailPage } from "@/features/trigger-instances/TriggerInstanceDetailPage";

export const Route = createFileRoute("/trigger-instances/$trgId")({
  component: TriggerInstanceDetailPage,
});
