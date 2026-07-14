import { useMutation, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";
import type { EndUser, UsersPage } from "@/lib/api-types";

/** useUsers lists the selected org's end-users (Slice 4, AC1) via the new
 * list-users-per-org endpoint, cursor-paginated — disabled while no org is
 * selected, the same convention every other org-scoped hook follows. */
export function useUsers(orgId: string | undefined): UseCursorPaginationResult<EndUser> {
  return useCursorPagination<EndUser>({
    queryKey: queryKeys.users.list(orgId ?? ""),
    fetchPage: (cursor) => fetchUsersPage(orgId as string, cursor),
    enabled: Boolean(orgId),
  });
}

function fetchUsersPage(orgId: string, cursor: string | undefined): Promise<UsersPage> {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return apiClient.get<UsersPage>(`/organizations/${orgId}/users${query}`);
}

/** useCreateUser creates an end-user in the selected org from the console
 * (Slice 4, AC2), reusing the same org-scoped CreateUser endpoint an org's
 * own server calls with its org API key. */
export function useCreateUser(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; externalId: string }) =>
      apiClient.post<EndUser>(`/organizations/${orgId}/users`, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.users.list(orgId) });
    },
  });
}
