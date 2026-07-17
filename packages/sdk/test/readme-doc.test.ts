import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const readmePath = resolve(packageDir, 'README.md');
const readme = readFileSync(readmePath, 'utf8');

const pkg = JSON.parse(readFileSync(resolve(packageDir, 'package.json'), 'utf8')) as {
  files?: string[];
  exports?: Record<string, unknown>;
  dependencies?: Record<string, string>;
  peerDependencies?: Record<string, string>;
  peerDependenciesMeta?: Record<string, { optional?: boolean }>;
};

describe('README install and quickstart', () => {
  it('shows the npm install command for the package', () => {
    expect(readme).toContain('npm install @beecon/sdk');
  });

  it('shows a minimal construct-the-client example', () => {
    expect(readme).toMatch(/new Beecon\(\s*\{/);
    expect(readme).toContain('apiKey');
    expect(readme).toContain('baseUrl');
  });
});

describe('README OpenAI adapter walkthrough', () => {
  it('shows importing toOpenAITools from the @beecon/sdk/openai subpath', () => {
    expect(readme).toContain("from '@beecon/sdk/openai'");
    expect(readme).toContain('toOpenAITools');
  });

  it('shows passing the generated tool defs to an OpenAI Responses-API call', () => {
    expect(readme).toContain('openai.responses.create');
    expect(readme).toContain('tools: toolDefs');
  });

  it('shows running the model tool-call back through Beecon via runToolCall', () => {
    expect(readme).toContain('runToolCall');
  });
});

describe('README Mastra adapter walkthrough', () => {
  it('shows importing toMastraTools from the @beecon/sdk/mastra subpath', () => {
    expect(readme).toContain("from '@beecon/sdk/mastra'");
    expect(readme).toContain('toMastraTools');
  });

  it('shows registering the generated tools on a Mastra agent', () => {
    expect(readme).toMatch(/new Agent\(/);
    expect(readme).toContain('tools:');
  });

  it('documents that a provider-level failure surfaces as MastraToolExecutionError', () => {
    expect(readme).toContain('MastraToolExecutionError');
  });
});

describe('README peer-dependency guidance', () => {
  it('states the consumer installs openai themselves for the openai subpath', () => {
    expect(readme).toMatch(/install\s+`?openai`?\s+yourself/i);
    expect(readme).toContain('npm install openai');
  });

  it('states the consumer installs @mastra/core themselves for the mastra subpath', () => {
    expect(readme).toMatch(/install\s+`?@mastra\/core`?\s+yourself/i);
    expect(readme).toContain('npm install @mastra/core');
  });

  it('describes both adapter dependencies as optional peer dependencies', () => {
    expect(readme).toMatch(/optional peer dependenc/i);
  });
});

describe('README Membrane-migration pointer', () => {
  it('states the Membrane migration guide is out of scope for this package and points to the importer strand', () => {
    expect(readme).toMatch(/migration guide/i);
    expect(readme).toMatch(/not\*\* part of this\s+package's docs|out of this strand|separate importer strand/i);
    expect(readme).toContain('importer strand');
  });
});

describe('tarball hygiene — examples are excluded from the published package', () => {
  it('ships only the dist directory (no examples, src, or test) in "files"', () => {
    expect(pkg.files).toEqual(['dist']);
    for (const entry of pkg.files ?? []) {
      expect(entry).not.toMatch(/^examples(\/|$)/);
    }
  });

  it('never resolves an export to the examples directory', () => {
    const exportTargets = JSON.stringify(pkg.exports ?? {});
    expect(exportTargets).not.toMatch(/\.\/examples\//);
  });
});

describe('package.json guarantees unchanged by this slice', () => {
  it('still declares zero runtime dependencies', () => {
    expect(pkg.dependencies ?? {}).toEqual({});
  });

  it('still marks both openai and @mastra/core peer dependencies as optional', () => {
    expect(pkg.peerDependenciesMeta?.openai?.optional).toBe(true);
    expect(pkg.peerDependenciesMeta?.['@mastra/core']?.optional).toBe(true);
  });
});
