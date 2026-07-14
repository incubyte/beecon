import { useEffect, useState, type ReactNode } from "react";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { Drawer } from "@/components/ui/Drawer";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import { TypeToConfirm } from "@/components/ui/TypeToConfirm";
import { useOrganizations } from "@/features/organizations/api";
import { ApiError } from "@/lib/api-client";

import { useConnection, useDeleteConnection, useDisableConnection, useReconnectConnection } from "./api";

export interface ConnectionDrawerProps {
  orgId: string;
  connectionId: string | null;
  onClose: () => void;
}

/** ConnectionDrawer is Slice 2's right-side detail panel (AC2): id,
 * integration, user, account, status, and a relative-with-hover created
 * timestamp, plus disable/delete/reconnect actions (context: "connections
 * list/get/disable/delete/reconnect"). Deleting a connection is the
 * highest-risk action here, so it goes through TypeToConfirm rather than a
 * plain confirm (DESIGN.md §7). */
export function ConnectionDrawer({ orgId, connectionId, onClose }: ConnectionDrawerProps) {
  const { data: connection, isLoading, isError } = useConnection(orgId, connectionId ?? undefined);
  const disableConnection = useDisableConnection(orgId);
  const deleteConnection = useDeleteConnection(orgId);
  const reconnectConnection = useReconnectConnection(orgId);
  const { items: organizations } = useOrganizations();

  const [reconnectRedirectUri, setReconnectRedirectUri] = useState("");
  const [reconnectRedirectUrl, setReconnectRedirectUrl] = useState<string | null>(null);

  useEffect(() => {
    setReconnectRedirectUri("");
    setReconnectRedirectUrl(null);
  }, [connectionId]);

  const allowedRedirectUris = organizations.find((org) => org.id === orgId)?.allowedRedirectUris ?? [];

  function handleDelete() {
    if (!connectionId) {
      return;
    }
    deleteConnection.mutate(connectionId, { onSuccess: onClose });
  }

  function handleReconnect() {
    if (!connectionId || !reconnectRedirectUri) {
      return;
    }
    reconnectConnection.mutate(
      { connectionId, redirectUri: reconnectRedirectUri },
      { onSuccess: (initiated) => setReconnectRedirectUrl(initiated.redirectUrl) },
    );
  }

  return (
    <Drawer
      open={connectionId !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      title="Connection detail"
      description={connectionId ? <CopyIdChip id={connectionId} /> : undefined}
    >
      {isLoading ? (
        <p className="text-sm text-text-secondary">Loading…</p>
      ) : isError ? (
        <p className="text-sm text-error-text">This connection could not be loaded.</p>
      ) : connection ? (
        <div className="flex flex-col gap-5">
          <dl className="flex flex-col gap-4">
            <DetailRow label="Status">
              <StatusBadge taxonomy="connection" status={connection.status} />
            </DetailRow>
            <DetailRow label="Integration">
              <span className="text-text">{connection.providerSlug}</span>
            </DetailRow>
            <DetailRow label="User">
              <CopyIdChip id={connection.userId} />
            </DetailRow>
            <DetailRow label="Account">
              <span className="text-text">
                {connection.account ? connection.account.displayName || connection.account.email : "—"}
              </span>
            </DetailRow>
            <DetailRow label="Created">
              <Timestamp iso={connection.createdAt} />
            </DetailRow>
          </dl>

          <div className="flex flex-col gap-3 border-t border-border pt-4">
            <button
              type="button"
              onClick={() => disableConnection.mutate(connection.id)}
              disabled={disableConnection.isPending || connection.status === "DISCONNECTED"}
              className="min-h-11 self-start rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {disableConnection.isPending ? "Disabling…" : "Disable connection"}
            </button>

            <TypeToConfirm
              trigger={
                <button
                  type="button"
                  className="min-h-11 self-start rounded-md border border-error-solid/40 px-4 text-sm font-medium text-error-text transition-colors hover:bg-error-solid/10 cursor-pointer"
                >
                  Delete connection
                </button>
              }
              title="Delete this connection?"
              description="This permanently removes the connection and cannot be undone."
              confirmText={connection.id}
              confirmLabel="Delete connection"
              onConfirm={handleDelete}
              isConfirming={deleteConnection.isPending}
            />
          </div>

          <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
            <p className="text-sm font-medium text-text">Reconnect</p>
            <label className="flex flex-col gap-1 text-sm text-text-secondary">
              Redirect URI
              <select
                value={reconnectRedirectUri}
                onChange={(event) => setReconnectRedirectUri(event.target.value)}
                className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
              >
                <option value="">Select a redirect URI</option>
                {allowedRedirectUris.map((uri) => (
                  <option key={uri} value={uri}>
                    {uri}
                  </option>
                ))}
              </select>
            </label>
            {allowedRedirectUris.length === 0 ? (
              <p className="text-xs text-text-muted">This organization has no allowed redirect URIs configured.</p>
            ) : null}
            <button
              type="button"
              onClick={handleReconnect}
              disabled={!reconnectRedirectUri || reconnectConnection.isPending}
              className="min-h-11 self-start rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {reconnectConnection.isPending ? "Reconnecting…" : "Reconnect"}
            </button>
            {reconnectConnection.isError ? (
              <p className="text-xs text-error-text">{errorMessage(reconnectConnection.error)}</p>
            ) : null}
            {reconnectRedirectUrl ? (
              <div className="rounded-md bg-surface-muted p-2">
                <p className="text-xs text-text-secondary">New connect link — hand this to the end user:</p>
                <p className="mt-1 break-all font-mono text-xs text-text">{reconnectRedirectUrl}</p>
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
    </Drawer>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-xs font-medium tracking-wide text-text-muted uppercase">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The reconnect request failed.";
  }
  return "The reconnect request failed.";
}
