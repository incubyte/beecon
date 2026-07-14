import { useState } from "react";

import type { ConfigDocument, ConfigImportApplyResult, ConfigImportMode, ConfigImportPlan } from "@/lib/api-types";

import { useApplyImport, useDryRunImport } from "./api";

function previewSignature(text: string, mode: ConfigImportMode): string {
  return `${mode}::${text}`;
}

/**
 * useConfigImportForm holds Slice 9's import flow state: the pasted/
 * uploaded document text, the merge/replace mode choice, and the dry-run
 * plan (PD46: "importing defaults to a dry-run that returns the diff it
 * would apply and writes nothing"). Apply is only ever enabled once a
 * dry-run has succeeded against the *exact* text and mode currently on
 * screen — editing the text or switching modes clears the stale preview,
 * so an operator can never apply a document they haven't just previewed.
 */
export function useConfigImportForm(orgId: string) {
  const [text, setTextValue] = useState("");
  const [mode, setModeValue] = useState<ConfigImportMode>("merge");
  const [parseError, setParseError] = useState<string | null>(null);
  const [plan, setPlan] = useState<ConfigImportPlan | null>(null);
  const [previewedSignature, setPreviewedSignature] = useState<string | null>(null);
  const [applied, setApplied] = useState<ConfigImportApplyResult | null>(null);

  const dryRun = useDryRunImport(orgId);
  const apply = useApplyImport(orgId);

  function clearPreview() {
    setPlan(null);
    setPreviewedSignature(null);
    setApplied(null);
  }

  function setText(next: string) {
    setTextValue(next);
    setParseError(null);
    clearPreview();
  }

  function setMode(next: ConfigImportMode) {
    setModeValue(next);
    clearPreview();
  }

  function parseDocument(): ConfigDocument | null {
    try {
      const parsed = JSON.parse(text) as ConfigDocument;
      setParseError(null);
      return parsed;
    } catch {
      setParseError("This isn't valid JSON. Check the file or pasted text and try again.");
      return null;
    }
  }

  function preview() {
    const document = parseDocument();
    if (!document) return;
    dryRun.mutate(
      { document, mode },
      {
        onSuccess: (result) => {
          setPlan(result);
          setPreviewedSignature(previewSignature(text, mode));
          setApplied(null);
        },
      },
    );
  }

  function applyImport() {
    const document = parseDocument();
    if (!document) return;
    apply.mutate(
      { document, mode },
      {
        onSuccess: (result) => {
          setApplied(result);
        },
      },
    );
  }

  const isStale = previewedSignature !== previewSignature(text, mode);
  const canApply = plan !== null && !isStale && text.trim() !== "";

  return {
    text,
    setText,
    mode,
    setMode,
    parseError,
    plan,
    applied,
    canApply,
    preview,
    applyImport,
    isPreviewing: dryRun.isPending,
    previewError: dryRun.isError ? dryRun.error : null,
    isApplying: apply.isPending,
    applyError: apply.isError ? apply.error : null,
  };
}
