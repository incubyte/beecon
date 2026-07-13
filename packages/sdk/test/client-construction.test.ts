import { describe, expect, it, vi } from 'vitest';
import { Beecon } from '../src/client.js';
import type { BeeconClient } from '../src/types.js';
import { asFetch, jsonResponse } from './support/responses.js';

describe('new Beecon()', () => {
  it('constructs a client wired to the given apiKey and baseUrl', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse([]));
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      fetch: asFetch(fetchMock),
    });

    await client.integrations.list();

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/integrations',
      expect.objectContaining({
        method: 'GET',
        headers: expect.objectContaining({ Authorization: 'Bearer beecon_sk_test_key' }),
      }),
    );
  });

  it('exposes users, integrations, connections, tools, logs, userTokens, and files sub-apis', () => {
    const client = new Beecon({ apiKey: 'beecon_sk_test_key', baseUrl: 'https://api.example.com' });

    expect(client.users.create).toBeInstanceOf(Function);
    expect(client.integrations.list).toBeInstanceOf(Function);
    expect(client.connections.initiate).toBeInstanceOf(Function);
    expect(client.connections.get).toBeInstanceOf(Function);
    expect(client.tools.execute).toBeInstanceOf(Function);
    expect(client.logs.list).toBeInstanceOf(Function);
    expect(client.userTokens.create).toBeInstanceOf(Function);
    expect(client.files.upload).toBeInstanceOf(Function);
  });
});

describe('BeeconClient interface mockability', () => {
  it('lets a consumer function accept a fully vi.fn()-built double in place of a real client', async () => {
    // Compile-time check lives in the type annotation below: if BeeconClient
    // ever grows a method this object doesn't implement, `satisfies
    // BeeconClient` fails `tsc --noEmit` (see tsconfig.test.json).
    const mockClient = {
      users: {
        create: vi.fn().mockResolvedValue({
          id: 'user_1',
          name: 'Ada Lovelace',
          externalId: '',
          createdAt: '2026-01-01T00:00:00Z',
        }),
      },
      integrations: {
        list: vi.fn().mockResolvedValue([]),
        getExpectedParams: vi.fn().mockResolvedValue({ providerName: 'Outlook', fields: [] }),
      },
      connections: {
        initiate: vi
          .fn()
          .mockResolvedValue({ id: 'conn_1', status: 'INITIATED', redirectUrl: 'https://x' }),
        get: vi.fn().mockResolvedValue({
          id: 'conn_1',
          status: 'ACTIVE',
          providerSlug: 'outlook',
          userId: 'user_1',
          createdAt: '2026-01-01T00:00:00Z',
        }),
        list: vi.fn().mockResolvedValue({ items: [] }),
        disable: vi.fn().mockResolvedValue({ id: 'conn_1', status: 'DISCONNECTED' }),
        delete: vi.fn().mockResolvedValue(undefined),
        reconnect: vi
          .fn()
          .mockResolvedValue({ id: 'conn_1', status: 'INITIATED', redirectUrl: 'https://x' }),
      },
      tools: {
        list: vi.fn().mockResolvedValue({ items: [] }),
        get: vi.fn().mockResolvedValue({
          slug: 'outlook-list-messages',
          name: 'List messages',
          description: '',
          inputSchema: {},
          outputSchema: {},
          deprecated: false,
          provider: { slug: 'outlook', name: 'Outlook', logo: '' },
        }),
        execute: vi.fn().mockResolvedValue({ successful: true, error: null, data: {} }),
      },
      logs: { list: vi.fn().mockResolvedValue({ entries: [] }) },
      userTokens: {
        create: vi.fn().mockReturnValue({ token: 'header.payload.sig', expiresAt: '2026-01-01T02:00:00Z' }),
      },
      files: {
        upload: vi.fn().mockResolvedValue({
          id: 'file_1',
          name: 'report.pdf',
          mimeType: 'application/pdf',
          size: 1024,
          downloadUrl: 'https://x/files/file_1/download',
        }),
      },
    } satisfies BeeconClient;

    async function createUser(client: BeeconClient, name: string): Promise<string> {
      const user = await client.users.create({ name });
      return user.id;
    }

    await expect(createUser(mockClient, 'Ada Lovelace')).resolves.toBe('user_1');
    expect(mockClient.users.create).toHaveBeenCalledWith({ name: 'Ada Lovelace' });
  });
});
