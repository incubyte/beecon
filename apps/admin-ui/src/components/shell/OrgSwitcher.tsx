import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Building2, Check, ChevronDown } from "lucide-react";

import { useOrganizations } from "@/features/organizations/api";
import { truncateId } from "@/lib/format";

/**
 * OrgSwitcher scopes every org-bound view via the `?org=` search param owned
 * by the root route (architecture doc §2.4): selecting an organization here
 * is the same action as selecting one from the Organizations list (Slice 1,
 * last AC) — both just set the same search param.
 */
export function OrgSwitcher() {
  const search = useSearch({ from: "__root__" });
  const navigate = useNavigate();
  const { items, isLoading } = useOrganizations();

  const selected = items.find((org) => org.id === search.org);

  function selectOrg(orgId: string | undefined) {
    void navigate({ to: ".", search: (prev) => ({ ...prev, org: orgId }) });
  }

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className="flex min-h-11 items-center gap-2 rounded-md border border-border px-3 text-sm text-text transition-colors hover:bg-surface-muted cursor-pointer"
        >
          <Building2 className="size-4 text-text-muted" aria-hidden="true" />
          <span className="max-w-48 truncate">
            {selected ? selected.name : "All organizations"}
          </span>
          <ChevronDown className="size-3.5 text-text-muted" aria-hidden="true" />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={6}
          className="z-20 min-w-64 rounded-lg border border-border bg-surface p-1 shadow-md"
        >
          <DropdownMenu.Item
            onSelect={() => selectOrg(undefined)}
            className="flex min-h-11 cursor-pointer items-center justify-between rounded-md px-3 text-sm text-text outline-none data-[highlighted]:bg-surface-muted"
          >
            <span>All organizations</span>
            {!search.org ? <Check className="size-4 text-primary" aria-hidden="true" /> : null}
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          {isLoading ? (
            <div className="px-3 py-2 text-sm text-text-muted">Loading…</div>
          ) : (
            items.map((org) => (
              <DropdownMenu.Item
                key={org.id}
                onSelect={() => selectOrg(org.id)}
                className="flex min-h-11 cursor-pointer items-center justify-between gap-2 rounded-md px-3 text-sm text-text outline-none data-[highlighted]:bg-surface-muted"
              >
                <span className="flex flex-col">
                  <span className="truncate">{org.name}</span>
                  <span className="font-mono text-xs text-text-muted">{truncateId(org.id)}</span>
                </span>
                {search.org === org.id ? <Check className="size-4 shrink-0 text-primary" aria-hidden="true" /> : null}
              </DropdownMenu.Item>
            ))
          )}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
