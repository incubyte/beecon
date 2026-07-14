import { createFileRoute } from "@tanstack/react-router";

import { TriggerDefinitionsPage } from "@/features/trigger-definitions/TriggerDefinitionsPage";

export const Route = createFileRoute("/trigger-definitions/")({
  component: TriggerDefinitionsPage,
});
