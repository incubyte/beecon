import { createHmac } from 'node:crypto';
import { MissingSigningSecretError, UserTokenExpiryTooLongError } from '../errors.js';
import type { CreateUserTokenInput, SigningSecretConfig, UserToken, UserTokensApi } from '../types.js';

const DEFAULT_EXPIRES_IN_SECONDS = 2 * 60 * 60;

// MAX_EXPIRES_IN_SECONDS is PD38a's 24-hour cap on a user token's lifetime
// (exp − iat): the server's VerifyUserToken rejects anything longer
// (server/internal/access/usertoken.go), so create refuses to mint one at
// all rather than handing back a token that can never verify.
const MAX_EXPIRES_IN_SECONDS = 24 * 60 * 60;

const inspectSymbol = Symbol.for('nodejs.util.inspect.custom');

interface UserTokenClaims {
  sub: string;
  iat: number;
  exp: number;
}

// UserTokensResource mints Beecon user tokens entirely locally (PD20): no
// HttpClient dependency, because minting never calls the network. The
// signing secret lives in a private class field — like HttpClient's #apiKey
// — so it is never assigned to an enumerable property and cannot appear in
// JSON.stringify(this), console.log(this), or any thrown error.
export class UserTokensResource implements UserTokensApi {
  readonly #signingSecret?: SigningSecretConfig;

  constructor(signingSecret?: SigningSecretConfig) {
    this.#signingSecret = signingSecret;
  }

  create(input: CreateUserTokenInput): UserToken {
    const signingSecret = this.requireSigningSecret();
    const expiresIn = input.expiresIn ?? DEFAULT_EXPIRES_IN_SECONDS;
    if (expiresIn > MAX_EXPIRES_IN_SECONDS) {
      throw new UserTokenExpiryTooLongError(expiresIn, MAX_EXPIRES_IN_SECONDS);
    }
    const issuedAt = Math.floor(Date.now() / 1000);
    const claims: UserTokenClaims = { sub: input.userId, iat: issuedAt, exp: issuedAt + expiresIn };
    return {
      token: mintHS256Token(signingSecret, claims),
      expiresAt: new Date(claims.exp * 1000).toISOString(),
    };
  }

  toJSON(): Record<string, never> {
    return {};
  }

  [inspectSymbol](): string {
    return 'UserTokensResource {}';
  }

  private requireSigningSecret(): SigningSecretConfig {
    if (!this.#signingSecret) {
      throw new MissingSigningSecretError();
    }
    return this.#signingSecret;
  }
}

// mintHS256Token builds the exact compact JWT the server's hand-rolled
// verifier (server/internal/access/usertoken.go) expects: a
// "header.payload.signature" string, each segment base64url (no padding)
// encoded, signed over "header.payload" with HMAC-SHA256 under the raw
// signing secret. The header always names HS256 and the configured kid.
function mintHS256Token(signingSecret: SigningSecretConfig, claims: UserTokenClaims): string {
  const headerSegment = base64UrlEncode(
    JSON.stringify({ alg: 'HS256', typ: 'JWT', kid: signingSecret.id }),
  );
  const payloadSegment = base64UrlEncode(JSON.stringify(claims));
  const signingInput = `${headerSegment}.${payloadSegment}`;
  const signatureSegment = base64UrlEncode(
    createHmac('sha256', signingSecret.secret).update(signingInput).digest(),
  );
  return `${signingInput}.${signatureSegment}`;
}

function base64UrlEncode(value: string | Buffer): string {
  return (typeof value === 'string' ? Buffer.from(value, 'utf8') : value).toString('base64url');
}
