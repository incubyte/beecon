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
});
