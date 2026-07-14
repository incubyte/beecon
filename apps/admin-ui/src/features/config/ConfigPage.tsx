import { useSearch } from "@tanstack/react-router";

import { EmptyState } from "@/components/ui/EmptyState";

import { ConfigExportSection } from "./ConfigExportSection";
import { ImportConfigSection } from "./ImportConfigSection";
import { ImportedSecretsQueue } from "./ImportedSecretsQueue";
import { useConfigImportForm } from "./useConfigImportForm";

/**
 * ConfigPage is Slice 9's GOVERN > Config export/import surface (PD46):
 * export downloads the selected org's governance, webhook endpoints, and
 * retention config as a secrets-free versioned JSON file; import previews a
 * dry-run diff (plus any unknown-integration-id warnings) before an
 * explicit merge/replace apply, and any freshly minted endpoint secrets the
 * apply creates are shown exactly once, one at a time, through the same
 * SecretOnceModal every other credential in this console uses.
 */
export function ConfigPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const importForm = useConfigImportForm(orgId ?? "");

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to export or import its config."
      />
    );
  }

  const secrets = importForm.applied?.secrets ?? [];

  return (
    <div className="flex flex-col gap-4 pb-20">
      <div>
        <h1 className="text-2xl font-semibold text-text">Config export / import</h1>
        <p className="text-sm text-text-secondary">
          Move this organization&rsquo;s governance, webhook endpoints, and retention config between installations.
          Exports never contain a secret or credential of any kind; imports always preview a dry-run diff before
          anything is written.
        </p>
      </div>

      <ConfigExportSection orgId={orgId} />
      <ImportConfigSection form={importForm} />

      {secrets.length > 0 ? <ImportedSecretsQueue secrets={secrets} /> : null}
    </div>
  );
}
