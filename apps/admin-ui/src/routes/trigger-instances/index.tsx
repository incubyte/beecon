import { createFileRoute } from "@tanstack/react-router";

import { TriggerInstancesPage } from "@/features/trigger-instances/TriggerInstancesPage";

export const Route = createFileRoute("/trigger-instances/")({
  component: TriggerInstancesPage,
});
