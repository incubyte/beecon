import { AlertTriangle } from "lucide-react";
import { useRef } from "react";

import { ApiError } from "@/lib/api-client";

import { ConfigChangeList } from "./ConfigChangeList";
import type { useConfigImportForm } from "./useConfigImportForm";

export interface ImportConfigSectionProps {
  form: ReturnType<typeof useConfigImportForm>;
}

/**
 * ImportConfigSection is Slice 9's import half (PD46, AC3-AC8): upload or
 * paste a previously exported config document, choose merge (default) or
 * replace, and preview the dry-run diff/plan plus any unknown-integration-id
 * warnings before applying anything — importing always defaults to, and
 * requires, a fresh dry-run preview of the exact document and mode about to
 * be applied. Nothing is ever written by "Preview changes"; only "Apply"
 * writes, and only once a matching preview exists.
 */
export function ImportConfigSection({ form }: ImportConfigSectionProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);

  function handleFileChosen(file: File | undefined) {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        form.setText(reader.result);
      }
    };
    reader.readAsText(file);
  }

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-border bg-surface p-4">
      <div>
        <h2 className="text-lg font-semibold text-text">Import</h2>
        <p className="text-sm text-text-secondary">
          Upload or paste a config document exported from this or another Beecon installation.{" "}
          <strong className="font-semibold text-text">
            Importing always previews a dry-run diff first — nothing is written until you review the plan below and
            choose Apply.
          </strong>
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-3">
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
          >
            Choose file…
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json"
            onChange={(event) => {
              handleFileChosen(event.target.files?.[0]);
              event.target.value = "";
            }}
            className="sr-only"
            aria-label="Upload config document"
          />
          <span className="text-sm text-text-secondary">or paste the JSON below</span>
        </div>
        <label htmlFor="config-import-text" className="sr-only">
          Config document JSON
        </label>
        <textarea
          id="config-import-text"
          value={form.text}
          onChange={(event) => form.setText(event.target.value)}
          rows={10}
          spellCheck={false}
          placeholder='{"schemaVersion": 1, "governance": { ... }, "endpoints": [ ... ], "retention": { ... }}'
          className="min-h-40 rounded-md border border-border-strong bg-surface px-3 py-2 font-mono text-xs text-text focus-visible:border-primary"
        />
        {form.parseError ? <p className="text-sm text-error-text">{form.parseError}</p> : null}
      </div>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-text">Mode</legend>
        <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-text">
          <input
            type="radio"
            name="config-import-mode"
            checked={form.mode === "merge"}
            onChange={() => form.setMode("merge")}
            className="size-4 cursor-pointer"
          />
          <span>Merge — upsert the document&rsquo;s settings, leave everything else untouched (default)</span>
        </label>
        <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-text">
          <input
            type="radio"
            name="config-import-mode"
            checked={form.mode === "replace"}
            onChange={() => form.setMode("replace")}
            className="size-4 cursor-pointer"
          />
          <span>
            Replace — make governance, endpoints, and retention match the document exactly, removing what it omits
          </span>
        </label>
      </fieldset>

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={form.preview}
          disabled={form.text.trim() === "" || form.isPreviewing}
          className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {form.isPreviewing ? "Previewing…" : "Preview changes (dry run)"}
        </button>
        <button
          type="button"
          onClick={form.applyImport}
          disabled={!form.canApply || form.isApplying}
          title={form.canApply ? undefined : "Preview changes first — Apply is only enabled for a document you've just previewed"}
          className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {form.isApplying ? "Applying…" : `Apply (${form.mode})`}
        </button>
      </div>

      {form.previewError ? <p className="text-sm text-error-text">{errorMessage(form.previewError)}</p> : null}
      {form.applyError ? <p className="text-sm text-error-text">{errorMessage(form.applyError)}</p> : null}

      {form.plan && !form.applied ? (
        <div className="flex flex-col gap-3 rounded-md border border-border-strong bg-surface-muted p-3">
          {form.plan.warnings.length > 0 ? (
            <div className="flex flex-col gap-1.5 rounded-md bg-warning-bg px-3 py-2.5 text-sm text-warning-text">
              <div className="flex items-center gap-2 font-medium">
                <AlertTriangle className="size-4 shrink-0" aria-hidden="true" />
                Referenced integrations not found in this installation
              </div>
              <ul className="list-disc pl-6">
                {form.plan.warnings.map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            </div>
          ) : null}
          <ConfigChangeList
            title="Dry-run plan — nothing has been written yet"
            changes={form.plan.plan}
            emptyDescription="This document matches the organization's current configuration exactly — applying it would change nothing."
          />
        </div>
      ) : null}

      {form.applied ? (
        <div className="flex flex-col gap-3 rounded-md border border-success-solid/30 bg-success-bg p-3">
          <p className="text-sm font-medium text-success-text">Import applied.</p>
          <ConfigChangeList title="Applied" changes={form.applied.applied} emptyDescription="Nothing changed." />
        </div>
      ) : null}
    </section>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The import request failed.";
  }
  return "The import request failed.";
}
