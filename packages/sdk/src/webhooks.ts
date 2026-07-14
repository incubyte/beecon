import { createHmac, timingSafeEqual } from 'node:crypto';
import { WebhookVerificationError } from './errors.js';
import type { VerifyWebhookHeaders, VerifyWebhookInput, WebhookEvent } from './types.js';

// SIGNATURE_PREFIX and TOLERANCE_SECONDS mirror the server's own signer
// (server/internal/delivery/signing.go) exactly — this is the interop
// contract (PD27): a hand-rolled Standard Webhooks verifier on node:crypto,
// zero runtime dependencies (FD4).
const SIGNATURE_PREFIX = 'v1,';
const TOLERANCE_SECONDS = 5 * 60;
const SECRET_PREFIX = 'whsec_';

// verify checks a delivery's Standard Webhooks headers against the raw
// payload it was sent with, and returns the typed PD32 event once the
// signature and timestamp both check out. It throws WebhookVerificationError
// for every other outcome — never the secret(s) it was given.
export function verify(input: VerifyWebhookInput): WebhookEvent {
  const headers = readSignatureHeaders(input.headers);
  assertTimestampWithinTolerance(headers.timestamp, input.now ?? new Date());
  const secrets = resolveSecrets(input);
  const content = signedContent(headers.id, headers.timestampRaw, input.payload);
  assertSignatureMatches(headers.signature, content, secrets);
  return parseEvent(input.payload);
}

interface SignatureHeaders {
  id: string;
  timestampRaw: string;
  timestamp: number;
  signature: string;
}

function readSignatureHeaders(headers: VerifyWebhookHeaders): SignatureHeaders {
  const id = getHeader(headers, 'webhook-id');
  const timestampRaw = getHeader(headers, 'webhook-timestamp');
  const signature = getHeader(headers, 'webhook-signature');
  if (!id || !timestampRaw || !signature) {
    throw new WebhookVerificationError(
      'malformed-header',
      'webhook-id, webhook-timestamp, and webhook-signature headers are all required',
    );
  }
  const timestamp = Number(timestampRaw);
  if (!Number.isInteger(timestamp)) {
    throw new WebhookVerificationError(
      'malformed-header',
      'webhook-timestamp must be a unix-seconds integer',
    );
  }
  return { id, timestampRaw, timestamp, signature };
}

function getHeader(headers: VerifyWebhookHeaders, name: string): string | undefined {
  if (typeof (headers as Headers).get === 'function') {
    return (headers as Headers).get(name) ?? undefined;
  }
  const record = headers as Record<string, string | string[] | undefined>;
  const value = record[name] ?? record[name.toLowerCase()] ?? record[name.toUpperCase()];
  return Array.isArray(value) ? value[0] : value;
}

function assertTimestampWithinTolerance(timestampSeconds: number, now: Date): void {
  const nowSeconds = Math.floor(now.getTime() / 1000);
  if (Math.abs(nowSeconds - timestampSeconds) > TOLERANCE_SECONDS) {
    throw new WebhookVerificationError(
      'timestamp',
      'webhook-timestamp is outside the ±5-minute tolerance',
    );
  }
}

function resolveSecrets(input: VerifyWebhookInput): string[] {
  if (input.secrets && input.secrets.length > 0) {
    return input.secrets;
  }
  if (input.secret) {
    return [input.secret];
  }
  throw new Error('webhooks.verify requires either `secret` or `secrets`.');
}

// signedContent reproduces "{id}.{timestamp}.{raw body}" exactly as the
// signer built it (server/internal/delivery/signing.go's signedContent) —
// timestampRaw is the header's own string, not a re-formatted number, so a
// verifier never diverges from the signer over formatting.
function signedContent(id: string, timestampRaw: string, payload: string): Buffer {
  return Buffer.from(`${id}.${timestampRaw}.${payload}`, 'utf8');
}

function assertSignatureMatches(signatureHeader: string, content: Buffer, secrets: string[]): void {
  const entries = parseSignatureEntries(signatureHeader);
  if (entries.length === 0) {
    throw new WebhookVerificationError(
      'signature',
      'webhook-signature has no recognized "v1," entry',
    );
  }
  const matched = entries.some((entry) => signatureMatchesAnySecret(content, entry, secrets));
  if (!matched) {
    throw new WebhookVerificationError(
      'tampered',
      'no provided secret produced a matching signature: the payload may have been altered after signing, or the secret is wrong',
    );
  }
}

function parseSignatureEntries(signatureHeader: string): string[] {
  return signatureHeader
    .split(' ')
    .filter((entry) => entry.startsWith(SIGNATURE_PREFIX))
    .map((entry) => entry.slice(SIGNATURE_PREFIX.length));
}

function signatureMatchesAnySecret(content: Buffer, candidateBase64: string, secrets: string[]): boolean {
  const candidate = Buffer.from(candidateBase64, 'base64');
  return secrets.some((secret) => {
    const expected = computeSignature(content, secret);
    return expected.length === candidate.length && timingSafeEqual(expected, candidate);
  });
}

function computeSignature(content: Buffer, secret: string): Buffer {
  return createHmac('sha256', decodeSecretKey(secret)).update(content).digest();
}

// decodeSecretKey derives the raw HMAC key from a whsec_-prefixed secret:
// strip the prefix, then base64-decode the remainder — the Standard
// Webhooks convention every off-the-shelf verifier expects (PD27), and
// exactly what server/internal/delivery/signing.go's decodeSecretKey does.
function decodeSecretKey(secret: string): Buffer {
  const trimmed = secret.startsWith(SECRET_PREFIX) ? secret.slice(SECRET_PREFIX.length) : secret;
  return Buffer.from(trimmed, 'base64');
}

function parseEvent(payload: string): WebhookEvent {
  return JSON.parse(payload) as WebhookEvent;
}
