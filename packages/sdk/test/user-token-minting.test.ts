import { createHmac } from 'node:crypto';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Beecon } from '../src/client.js';
import { MissingSigningSecretError } from '../src/errors.js';
import { UserTokensResource } from '../src/resources/userTokens.js';
import { asFetch } from './support/responses.js';

// mintExpectedToken independently reproduces the wire format
// UserTokensResource.create is documented to produce (see the comment above
// mintHS256Token in ../src/resources/userTokens.ts): a compact
// "header.payload.signature" JWT, each segment base64url (no padding)
// encoded, signed over "header.payload" with HMAC-SHA256 under the raw
// signing secret. This helper never imports the production mint function —
// it recomputes the same construction from scratch so the assertion below
// isn't circular.
function mintExpectedToken(
  secret: string,
  kid: string,
  claims: { sub: string; iat: number; exp: number },
): string {
  const b64 = (value: string): string => Buffer.from(value, 'utf8').toString('base64url');
  const header = b64(JSON.stringify({ alg: 'HS256', typ: 'JWT', kid }));
  const payload = b64(JSON.stringify(claims));
  const signingInput = `${header}.${payload}`;
  const signature = createHmac('sha256', secret).update(signingInput).digest().toString('base64url');
  return `${signingInput}.${signature}`;
}

function decodeSegment(segment: string): Record<string, unknown> {
  return JSON.parse(Buffer.from(segment, 'base64url').toString('utf8')) as Record<string, unknown>;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('userTokens.create — wire-format interop with the server verifier', () => {
  // These fixed vectors mirror server/internal/access/usertoken_test.go:
  // userTokenTestNow (2026-06-01T12:00:00Z, i.e. Unix 1780315200) and
  // testUserID ("user_ada") are the exact clock and subject the Go suite
  // pins its HS256 construction to. The secret/kid below are this test's own
  // fixture (the Go test issues its secret through the facade, so its literal
  // value can't be mirrored) but are shaped like a real usk_ signing secret.
  // encoding/json (Go) marshals map keys in sorted order while
  // JSON.stringify (here, and in production) preserves declared order — the
  // header's byte layout can differ between the two languages' encoders, but
  // that's fine: both are valid JSON and the Go verifier decodes fields by
  // name rather than position, and recomputes the HMAC over the token's own
  // header/payload bytes rather than any fixed string.
  const vectorSecretId = 'usk_vector0001';
  const vectorSecret = 'beecon-usertoken-vector-secret-32bytes!!';
  const vectorUserId = 'user_ada';
  const vectorIssuedAtUnix = 1780315200; // 2026-06-01T12:00:00Z, userTokenTestNow
  const vectorExpiresAtUnix = vectorIssuedAtUnix + 7200; // default 2h expiry

  // This exact string is committed as the shared cross-language contract
  // artifact in server/internal/access/usertoken_sdk_interop_test.go
  // (sdkMintedUserToken): it was literally produced by this SDK's
  // UserTokensResource.create over the vectors above, and that Go test
  // asserts the real hand-rolled verifier (VerifyUserToken) accepts it. A
  // same-language recomputation (mintExpectedToken below) can share a
  // misreading of the spec with the code it's checking, so byte-equality
  // against this independently-verified literal is the real proof, not the
  // recomputation alone.
  const committedGoldenToken =
    'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6InVza192ZWN0b3IwMDAxIn0.' +
    'eyJzdWIiOiJ1c2VyX2FkYSIsImlhdCI6MTc4MDMxNTIwMCwiZXhwIjoxNzgwMzIyNDAwfQ.' +
    'O2x9y-zik5gpItp-3qNxuOJLaUO3MFg1orqWca1WGmo';

  it('mints the exact token committed as the shared contract artifact with the Go verifier suite', () => {
    vi.spyOn(Date, 'now').mockReturnValue(vectorIssuedAtUnix * 1000);
    const userTokens = new UserTokensResource({ id: vectorSecretId, secret: vectorSecret });

    const result = userTokens.create({ userId: vectorUserId });

    expect(result.token).toBe(committedGoldenToken);
  });

  it('mints the byte-identical token an independent HS256 computation over the same vectors produces', () => {
    vi.spyOn(Date, 'now').mockReturnValue(vectorIssuedAtUnix * 1000);
    const userTokens = new UserTokensResource({ id: vectorSecretId, secret: vectorSecret });

    const result = userTokens.create({ userId: vectorUserId });

    const expectedToken = mintExpectedToken(vectorSecret, vectorSecretId, {
      sub: vectorUserId,
      iat: vectorIssuedAtUnix,
      exp: vectorExpiresAtUnix,
    });
    expect(result.token).toBe(expectedToken);
    expect(result.token).toBe(committedGoldenToken);
  });

  it('base64url-encodes every segment without padding characters', () => {
    vi.spyOn(Date, 'now').mockReturnValue(vectorIssuedAtUnix * 1000);
    const userTokens = new UserTokensResource({ id: vectorSecretId, secret: vectorSecret });

    const result = userTokens.create({ userId: vectorUserId });

    expect(result.token.split('.')).toHaveLength(3);
    expect(result.token).not.toContain('=');
    expect(result.token).not.toContain('+');
    expect(result.token).not.toContain('/');
  });
});

describe('userTokens.create — claims and defaults', () => {
  it('names the configured signing secret id as the header kid', () => {
    const userTokens = new UserTokensResource({ id: 'usk_config_1', secret: 'sekrit' });

    const result = userTokens.create({ userId: 'user_1' });

    const [headerSegment] = result.token.split('.');
    expect(decodeSegment(headerSegment)).toMatchObject({ alg: 'HS256', kid: 'usk_config_1' });
  });

  it('sets the payload sub claim to the requested userId', () => {
    const userTokens = new UserTokensResource({ id: 'usk_config_1', secret: 'sekrit' });

    const result = userTokens.create({ userId: 'user_ada' });

    const [, payloadSegment] = result.token.split('.');
    expect(decodeSegment(payloadSegment).sub).toBe('user_ada');
  });

  it('defaults expiry to exactly 2 hours (7200s) after issuance', () => {
    const userTokens = new UserTokensResource({ id: 'usk_config_1', secret: 'sekrit' });

    const result = userTokens.create({ userId: 'user_1' });

    const [, payloadSegment] = result.token.split('.');
    const claims = decodeSegment(payloadSegment) as { iat: number; exp: number };
    expect(claims.exp - claims.iat).toBe(7200);
  });

  it('reflects the default 2h expiry in the returned expiresAt timestamp', () => {
    const userTokens = new UserTokensResource({ id: 'usk_config_1', secret: 'sekrit' });

    const result = userTokens.create({ userId: 'user_1' });

    const [, payloadSegment] = result.token.split('.');
    const claims = decodeSegment(payloadSegment) as { exp: number };
    expect(result.expiresAt).toBe(new Date(claims.exp * 1000).toISOString());
  });

  it('honors a custom expiresIn instead of the 2h default', () => {
    const userTokens = new UserTokensResource({ id: 'usk_config_1', secret: 'sekrit' });

    const result = userTokens.create({ userId: 'user_1', expiresIn: 300 });

    const [, payloadSegment] = result.token.split('.');
    const claims = decodeSegment(payloadSegment) as { iat: number; exp: number };
    expect(claims.exp - claims.iat).toBe(300);
  });
});

describe('userTokens.create — never calls the network', () => {
  it("never invokes the client's fetch implementation while minting, unlike every other resource method", () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('network should never be reached'));
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      fetch: asFetch(fetchMock),
      signingSecret: { id: 'usk_config_1', secret: 'sekrit' },
    });

    client.userTokens.create({ userId: 'user_1' });

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('returns synchronously rather than a Promise', () => {
    const userTokens = new UserTokensResource({ id: 'usk_config_1', secret: 'sekrit' });

    const result = userTokens.create({ userId: 'user_1' });

    expect(result).not.toBeInstanceOf(Promise);
    expect(typeof result.token).toBe('string');
  });
});

describe('userTokens.create — misconfiguration', () => {
  it('throws MissingSigningSecretError when constructed with no signing secret', () => {
    const userTokens = new UserTokensResource();

    expect(() => userTokens.create({ userId: 'user_1' })).toThrow(MissingSigningSecretError);
  });

  it('names the fix (constructing Beecon with signingSecret) in the error message', () => {
    const userTokens = new UserTokensResource();

    expect(() => userTokens.create({ userId: 'user_1' })).toThrow(/signingSecret/);
  });
});
