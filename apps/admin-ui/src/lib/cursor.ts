import { useInfiniteQuery, type QueryKey } from "@tanstack/react-query";

import type { Page } from "./api-types";

export interface UseCursorPaginationOptions<T> {
  queryKey: QueryKey;
  fetchPage: (cursor: string | undefined) => Promise<Page<T>>;
  enabled?: boolean;
}

export interface UseCursorPaginationResult<T> {
  items: T[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  hasMore: boolean;
  isLoadingMore: boolean;
  loadMore: () => void;
  refetch: () => void;
}

/**
 * useCursorPagination is the shared "load more" hook over Beecon's opaque
 * base64 nextCursor (DESIGN.md §6): it never exposes numbered pages, only
 * an accumulated item list and a loadMore action, backed by TanStack
 * Query's own cursor-shaped useInfiniteQuery so refetch/cache/invalidation
 * behavior is battle-tested rather than hand-rolled.
 */
export function useCursorPagination<T>({
  queryKey,
  fetchPage,
  enabled = true,
}: UseCursorPaginationOptions<T>): UseCursorPaginationResult<T> {
  const query = useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam }) => fetchPage(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
    enabled,
  });

  const items = query.data?.pages.flatMap((page) => page.items) ?? [];

  return {
    items,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    hasMore: Boolean(query.hasNextPage),
    isLoadingMore: query.isFetchingNextPage,
    loadMore: () => {
      void query.fetchNextPage();
    },
    refetch: () => {
      void query.refetch();
    },
  };
}
