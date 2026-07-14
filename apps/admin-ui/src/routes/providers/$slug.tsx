import { createFileRoute } from "@tanstack/react-router";

import { ProviderDefinitionDetailPage } from "@/features/providers/ProviderDefinitionDetailPage";

export const Route = createFileRoute("/providers/$slug")({
  component: ProviderDefinitionDetailPage,
});
