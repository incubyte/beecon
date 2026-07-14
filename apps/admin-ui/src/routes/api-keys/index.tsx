import { createFileRoute } from "@tanstack/react-router";

import { ApiKeysPage } from "@/features/api-keys/ApiKeysPage";

export const Route = createFileRoute("/api-keys/")({
  component: ApiKeysPage,
});
