import type { ApiKeyListing } from "@/lib/api-types";

export type ApiKeyDisplayStatus = "ACTIVE" | "ROTATING" | "REVOKED";

/** deriveApiKeyStatus turns a key's raw revokedAt/rotatedAt/overlapExpiresAt
 * timestamps (KeyListing, access/facade.go) into the single display status
 * StatusBadge's "apiKey" taxonomy renders (Slice 4, AC3): REVOKED wins
 * outright; ROTATING while a just-rotated key's outgoing secret is still
 * inside its overlap window (PD23); ACTIVE otherwise. */
export function deriveApiKeyStatus(key: ApiKeyListing, now: Date = new Date()): ApiKeyDisplayStatus {
  if (key.revokedAt) {
    return "REVOKED";
  }
  if (key.rotatedAt && key.overlapExpiresAt && new Date(key.overlapExpiresAt).getTime() > now.getTime()) {
    return "ROTATING";
  }
  return "ACTIVE";
}
