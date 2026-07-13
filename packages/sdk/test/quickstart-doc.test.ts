import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const testDir = dirname(fileURLToPath(import.meta.url));
const quickstartPath = resolve(testDir, '../../../docs/quickstart.md');

describe('quickstart document', () => {
  it('exists at docs/quickstart.md', () => {
    expect(() => readFileSync(quickstartPath, 'utf8')).not.toThrow();
  });

  it('walks the popup connect flow using the SDK\'s real exported names', () => {
    const content = readFileSync(quickstartPath, 'utf8');

    for (const mustMention of [
      'connections.initiate',
      'redirectUrl',
      'popup',
      'outlook-list-messages',
      'tools.execute',
      'connections.get',
      'BeeconApiError',
    ]) {
      expect(content).toContain(mustMention);
    }
  });

  it('has a section covering the browser-token connect flow, referencing userTokens.create and signingSecret', () => {
    const content = readFileSync(quickstartPath, 'utf8');

    expect(content).toMatch(/##\s+The browser-token connect flow/i);
    expect(content).toContain('userTokens.create');
    expect(content).toContain('signingSecret');
    expect(content).toContain('MissingSigningSecretError');
  });

  it('has a section covering connecting Hubspot, referencing the real hubspot providerSlug', () => {
    const content = readFileSync(quickstartPath, 'utf8');

    expect(content).toMatch(/##\s+Connecting Hubspot/i);
    expect(content).toContain("providerSlug === 'hubspot'");
  });

  it('has a section covering paging a list tool, referencing nextCursor and tools.list', () => {
    const content = readFileSync(quickstartPath, 'utf8');

    expect(content).toMatch(/##\s+Paging through a list tool/i);
    expect(content).toContain('nextCursor');
    expect(content).toContain('tools.list');
    expect(content).toContain('hubspot-list-contacts');
  });

  it('has a section covering uploading a file into a tool call, referencing files.upload', () => {
    const content = readFileSync(quickstartPath, 'utf8');

    expect(content).toMatch(/##\s+Uploading a file into a tool call/i);
    expect(content).toContain('files.upload');
    expect(content).toContain('hubspot-upload-file');
  });
});
