import { useState } from "react";

import { SecretOnceModal } from "@/components/ui/SecretOnceModal";
import type { ConfigImportedSecret } from "@/lib/api-types";

export interface ImportedSecretsQueueProps {
  secrets: ConfigImportedSecret[];
}

/**
 * ImportedSecretsQueue shows each freshly minted endpoint secret an apply
 * created (Slice 9, PD46: "endpoints created by an import get freshly
 * generated secrets, shown once, since secrets are never exported") one at
 * a time through the same SecretOnceModal every other credential in this
 * console uses. An import that creates several endpoints in one apply still
 * shows every secret, just sequentially — the acknowledge-then-dismiss gate
 * (SecretOnceModal's own checkbox) applies to each one individually before
 * the next appears.
 */
export function ImportedSecretsQueue({ secrets }: ImportedSecretsQueueProps) {
  const [index, setIndex] = useState(0);

  if (index >= secrets.length) {
    return null;
  }

  const current = secrets[index];

  return (
    <SecretOnceModal
      open
      onDismiss={() => setIndex((value) => value + 1)}
      title={`New webhook endpoint secret (${index + 1} of ${secrets.length})`}
      secret={current.secret}
      helpText={`This is the whsec_ signing secret this import just minted for endpoint ${current.wepId}. It will never be shown again.`}
      fileNamePrefix={`beecon-import-secret-${current.wepId}`}
    />
  );
}
