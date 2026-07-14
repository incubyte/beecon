import { createFileRoute } from "@tanstack/react-router";

import { GovernancePage } from "@/features/governance/GovernancePage";

export const Route = createFileRoute("/governance/")({
  component: GovernancePage,
});
