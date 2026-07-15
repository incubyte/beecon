import * as Dialog from "@radix-ui/react-dialog";
import { useNavigate } from "@tanstack/react-router";
import { Command } from "cmdk";
import { Building2, LogOut, Search } from "lucide-react";
import { useEffect, useState } from "react";

import { useOrganizations } from "@/features/organizations/api";
import { useSignOut } from "@/lib/auth";

/**
 * CommandPalette is the Cmd/Ctrl-K entry point DESIGN.md §5/§7 specifies:
 * jump to an organization, or sign out. Later slices extend the command
 * list (entities, actions) as their areas land — Slice 1 wires the palette
 * itself plus the one entity this slice knows about.
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const { items } = useOrganizations();
  const signOut = useSignOut();

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        setOpen((value) => !value);
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  function jumpToOrg(orgId: string) {
    setOpen(false);
    void navigate({ to: "/organizations", search: (prev) => ({ ...prev, org: orgId }) });
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex min-h-11 items-center gap-2 rounded-md border border-border px-3 text-sm text-text-secondary transition-colors hover:bg-surface-muted cursor-pointer"
      >
        <Search className="size-4" aria-hidden="true" />
        <span className="hidden sm:inline">Jump to…</span>
        <kbd className="hidden rounded border border-border-strong px-1.5 py-0.5 font-mono text-xs text-text-muted sm:inline">
          {"⌘K"}
        </kbd>
      </button>

      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-30 bg-black/40" />
          <Dialog.Content
            className="fixed top-24 left-1/2 z-40 w-full max-w-lg -translate-x-1/2 rounded-lg border border-border bg-surface shadow-lg"
            aria-describedby={undefined}
          >
            <Dialog.Title className="sr-only">Command palette</Dialog.Title>
            <Command label="Command palette">
              <Command.Input
                placeholder="Search organizations, or type a command…"
                className="min-h-11 w-full border-b border-border bg-transparent px-4 text-base text-text outline-none placeholder:text-text-muted"
              />
              <Command.List className="max-h-80 overflow-y-auto p-2">
                <Command.Empty className="px-3 py-6 text-center text-sm text-text-muted">
                  No results found.
                </Command.Empty>
                <Command.Group heading="Organizations" className="text-xs text-text-muted">
                  {items.map((org) => (
                    <Command.Item
                      key={org.id}
                      value={`${org.name} ${org.id}`}
                      onSelect={() => jumpToOrg(org.id)}
                      className="flex min-h-11 cursor-pointer items-center gap-2.5 rounded-md px-3 text-sm text-text data-[selected=true]:bg-surface-muted"
                    >
                      <Building2 className="size-4 text-text-muted" aria-hidden="true" />
                      {org.name}
                    </Command.Item>
                  ))}
                </Command.Group>
                <Command.Group heading="Session" className="text-xs text-text-muted">
                  <Command.Item
                    value="sign out"
                    onSelect={() => {
                      setOpen(false);
                      signOut();
                    }}
                    className="flex min-h-11 cursor-pointer items-center gap-2.5 rounded-md px-3 text-sm text-text data-[selected=true]:bg-surface-muted"
                  >
                    <LogOut className="size-4 text-text-muted" aria-hidden="true" />
                    Sign out
                  </Command.Item>
                </Command.Group>
              </Command.List>
            </Command>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
