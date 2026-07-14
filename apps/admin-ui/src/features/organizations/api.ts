import type { Organization, OrganizationsPage } from "@/lib/api-types";
import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";

/** useOrganizations lists every organization in the installation (Slice 1,
 * PD40) via the admin-guarded GET /api/v1/organizations, cursor-paginated. */
export function useOrganizations(): UseCursorPaginationResult<Organization> {
  return useCursorPagination<Organization>({
    queryKey: queryKeys.organizations.list(),
    fetchPage: fetchOrganizationsPage,
  });
}

function fetchOrganizationsPage(cursor: string | undefined): Promise<OrganizationsPage> {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return apiClient.get<OrganizationsPage>(`/organizations${query}`);
}
