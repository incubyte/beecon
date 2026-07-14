import { createFileRoute } from "@tanstack/react-router";

import { OrganizationsPage } from "@/features/organizations/OrganizationsPage";

export const Route = createFileRoute("/organizations/")({
  component: OrganizationsPage,
});
