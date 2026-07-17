import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  Beecon,
  BeeconApiError,
  MissingSigningSecretError,
  RateLimitedError,
  UserTokenExpiryTooLongError,
  WebhookVerificationError,
  webhooks,
} from '../src/index.js';
import type { BeeconClient } from '../src/index.js';

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const pkg = JSON.parse(readFileSync(resolve(packageDir, 'package.json'), 'utf8')) as {
  version: string;
  license?: string;
  repository?: { type: string; url: string; directory: string };
  type?: string;
  files?: string[];
  exports?: Record<string, unknown>;
  engines?: { node?: string };
  publishConfig?: { access?: string };
  scripts?: Record<string, string>;
  dependencies?: Record<string, string>;
  peerDependencies?: Record<string, string>;
};

// --- Zero-runtime-dependency core ------------------------------------------

describe('zero-runtime-dependency core', () => {
  it('declares no runtime dependencies, so npm install adds zero transitive deps', () => {
    expect(pkg.dependencies ?? {}).toEqual({});
  });
});

// --- Publish-readiness metadata ---------------------------------------------

describe('publish-readiness metadata', () => {
  it('declares an MIT license', () => {
    expect(pkg.license).toBe('MIT');
  });

  it('declares a repository pointing at the packages/sdk directory of the beecon repo', () => {
    expect(pkg.repository).toMatchObject({
      type: 'git',
      url: expect.stringContaining('github.com'),
      directory: 'packages/sdk',
    });
  });

  it('ships only the dist directory in the published tarball', () => {
    expect(pkg.files).toEqual(['dist']);
  });

  it('is on a 0.x version line, matching the no-stability-promise decision', () => {
    const major = Number(pkg.version.split('.')[0]);
    expect(major).toBe(0);
  });

  it('is configured to publish to the public npm registry (required for a scoped package)', () => {
    expect(pkg.publishConfig?.access).toBe('public');
  });

  it('gates npm publish on a build and the full test suite via prepublishOnly', () => {
    const gate = pkg.scripts?.prepublishOnly ?? '';
    expect(gate).toContain('build');
    expect(gate).toContain('test');
  });

  it('ships as an ESM package', () => {
    expect(pkg.type).toBe('module');
  });

  it('declares a minimum supported Node engine of 18 or newer', () => {
    expect(pkg.engines?.node).toBe('>=18');
  });
});

// --- Subpath export: ./agent (triggers/webhook typed helpers) ---------------

describe('subpath export — ./agent', () => {
  it('declares an ./agent export with its own types and build output, alongside the existing subpaths', () => {
    expect(pkg.exports?.['./agent']).toEqual({
      types: './dist/agent.d.ts',
      import: './dist/agent.js',
    });
  });

  it('adds no peer dependency beyond the existing openai/@mastra/core adapter peers', () => {
    expect(Object.keys(pkg.peerDependencies ?? {}).sort()).toEqual(['@mastra/core', 'openai']);
  });
});

// --- Tarball hygiene ---------------------------------------------------------
//
// The definitive check is `npm pack --dry-run` against the built tarball,
// which is a packaging-tool operation rather than something worth spinning up
// inside the vitest suite (it needs a prior `npm run build`, and duplicating
// npm's own packing logic here would just re-test npm). These assertions
// instead check the two inputs that determine tarball contents: `files` and
// `exports` must never point at `src`/`test`.

describe('tarball hygiene', () => {
  it('never lists a src or test path in the files field shipped to consumers', () => {
    for (const entry of pkg.files ?? []) {
      expect(entry).not.toMatch(/^(src|test)(\/|$)/);
    }
  });

  it('never resolves an export to a src or test path', () => {
    const exportTargets = JSON.stringify(pkg.exports ?? {});
    expect(exportTargets).not.toMatch(/\.\/(src|test)\//);
  });
});

// --- Public surface resolves -------------------------------------------------

describe('the package entry point exposes the full SDK surface', () => {
  it('constructs a real Beecon client wired to every existing sub-API', () => {
    const client = new Beecon({ apiKey: 'beecon_sk_test_key', baseUrl: 'https://api.example.com' });

    expect(client.users.create).toBeInstanceOf(Function);
    expect(client.integrations.list).toBeInstanceOf(Function);
    expect(client.connections.initiate).toBeInstanceOf(Function);
    expect(client.tools.execute).toBeInstanceOf(Function);
    expect(client.logs.list).toBeInstanceOf(Function);
    expect(client.userTokens.create).toBeInstanceOf(Function);
    expect(client.files.upload).toBeInstanceOf(Function);
    expect(client.triggers.create).toBeInstanceOf(Function);
    expect(client.webhookEndpoint.set).toBeInstanceOf(Function);
    expect(client.events.list).toBeInstanceOf(Function);
  });

  it('type-checks the constructed client against the BeeconClient type re-exported from the entry point', () => {
    const client = new Beecon({ apiKey: 'beecon_sk_test_key', baseUrl: 'https://api.example.com' });

    // If index.ts's re-exported BeeconClient type ever drops or diverges from
    // a sub-API Beecon actually implements, this assignment fails
    // `tsc --noEmit -p tsconfig.test.json` (the project's typecheck script) —
    // proving the root export's types resolve against the same surface as
    // the source, per this slice's AC.
    const asPublishedContract: BeeconClient = client;

    expect(asPublishedContract.triggers.listDefinitions).toBeInstanceOf(Function);
    expect(asPublishedContract.webhookEndpoint.rotateSecret).toBeInstanceOf(Function);
    expect(asPublishedContract.events.redeliver).toBeInstanceOf(Function);
  });

  it('exports usable error classes from the entry point, each a real Error subclass', () => {
    const apiError = new BeeconApiError(404, { code: 'not_found', message: 'connection not found' });
    expect(apiError).toBeInstanceOf(Error);
    expect(apiError.status).toBe(404);
    expect(apiError.code).toBe('not_found');

    const rateLimited = new RateLimitedError(30, { code: 'rate_limited', message: 'throttled' });
    expect(rateLimited).toBeInstanceOf(BeeconApiError);
    expect(rateLimited.retryAfter).toBe(30);

    expect(new MissingSigningSecretError()).toBeInstanceOf(Error);
    expect(new UserTokenExpiryTooLongError(90000, 86400)).toBeInstanceOf(Error);
    expect(new WebhookVerificationError('tampered', 'bad signature')).toBeInstanceOf(Error);
  });

  it('exports the webhooks namespace from the entry point, wired to a real verifier', () => {
    expect(webhooks.verify).toBeInstanceOf(Function);

    // Exercises the real verification path (no signature/timestamp given) so
    // this asserts behavior, not just presence of the function.
    let caught: unknown;
    try {
      webhooks.verify({ payload: '{}', headers: {}, secret: 'whsec_AA==' });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(WebhookVerificationError);
    expect((caught as InstanceType<typeof WebhookVerificationError>).reason).toBe('malformed-header');
  });
});
