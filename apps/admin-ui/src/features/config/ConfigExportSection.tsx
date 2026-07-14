import { Download } from "lucide-react";

import { ApiError } from "@/lib/api-client";
import type { ConfigDocument } from "@/lib/api-types";

import { useExportConfig } from "./api";

export interface ConfigExportSectionProps {
  orgId: string;
}

/**
 * ConfigExportSection is Slice 9's export half (PD46, AC1/AC2): downloads
 * the selected organization's governance, webhook endpoint URLs and
 * event-type filters, and retention config as a versioned JSON file — never
 * an API-key/webhook secret, credential, connection, user token, or
 * provider definition, since the backend's own ConfigDocument has no field
 * for any of them.
 */
export function ConfigExportSection({ orgId }: ConfigExportSectionProps) {
  const exportConfig = useExportConfig(orgId);

  function handleDownload() {
    exportConfig.mutate(undefined, {
      onSuccess: (document) => downloadConfigDocument(document, orgId),
    });
  }

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4">
      <div>
        <h2 className="text-lg font-semibold text-text">Export</h2>
        <p className="text-sm text-text-secondary">
          Download this organization&rsquo;s governance, webhook endpoint URLs and event-type filters, and retention
          config as a versioned JSON file. It never contains an API key, webhook signing secret, credential,
          connection, user token, or provider definition — there is nothing in the file to leak.
        </p>
      </div>

      {exportConfig.isError ? <p className="text-sm text-error-text">{errorMessage(exportConfig.error)}</p> : null}

      <div>
        <button
          type="button"
          onClick={handleDownload}
          disabled={exportConfig.isPending}
          className="flex min-h-11 w-fit items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          <Download className="size-4" aria-hidden="true" />
          {exportConfig.isPending ? "Preparing download…" : "Download export"}
        </button>
      </div>
    </section>
  );
}

function downloadConfigDocument(configDocument: ConfigDocument, orgId: string) {
  const blob = new Blob([JSON.stringify(configDocument, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `beecon-config-${orgId}.json`;
  anchor.click();
  URL.revokeObjectURL(url);
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The config export could not be downloaded.";
  }
  return "The config export could not be downloaded.";
}
