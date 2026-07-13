import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { FilesResource } from '../src/resources/files.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): FilesResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new FilesResource(http);
}

const uploadedFileResponse = {
  id: 'file_1',
  name: 'invoice.pdf',
  mimeType: 'application/pdf',
  size: 3,
  downloadUrl: 'https://api.example.com/api/v1/files/file_1/download',
};

describe('files.upload', () => {
  it('POSTs a multipart/form-data body to /api/v1/files', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);

    await files.upload({
      fileName: 'invoice.pdf',
      mimeType: 'application/pdf',
      content: new Uint8Array([1, 2, 3]),
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/files');
    expect(init.method).toBe('POST');
    expect(init.body).toBeInstanceOf(FormData);
  });

  it('carries the file bytes, name, and mime type as the FormData "file" part', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);

    await files.upload({
      fileName: 'invoice.pdf',
      mimeType: 'application/pdf',
      content: new Uint8Array([1, 2, 3]),
    });

    const [, init] = fetchMock.mock.calls[0];
    const form = init.body as FormData;
    const filePart = form.get('file') as File;
    expect(filePart.name).toBe('invoice.pdf');
    expect(filePart.type).toBe('application/pdf');
    expect(filePart.size).toBe(3);
  });

  it('defaults the mime type to application/octet-stream when none is given', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);

    await files.upload({ fileName: 'blob.bin', content: new Uint8Array([1]) });

    const [, init] = fetchMock.mock.calls[0];
    const filePart = (init.body as FormData).get('file') as File;
    expect(filePart.type).toBe('application/octet-stream');
  });

  it('does not set a JSON Content-Type header, letting fetch derive the multipart boundary', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);

    await files.upload({ fileName: 'invoice.pdf', content: new Uint8Array([1]) });

    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers['Content-Type']).toBeUndefined();
    expect(init.headers.Authorization).toBe('Bearer beecon_sk_test_key');
  });

  it('accepts a Blob as the file content directly', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);

    await files.upload({
      fileName: 'invoice.pdf',
      mimeType: 'application/pdf',
      content: new Blob([new Uint8Array([1, 2, 3])], { type: 'application/pdf' }),
    });

    const [, init] = fetchMock.mock.calls[0];
    const filePart = (init.body as FormData).get('file') as File;
    expect(filePart.size).toBe(3);
    expect(filePart.type).toBe('application/pdf');
  });

  it('accepts a plain ArrayBuffer as the file content', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);
    const arrayBuffer = new Uint8Array([1, 2, 3]).buffer;

    await files.upload({ fileName: 'invoice.pdf', mimeType: 'application/pdf', content: arrayBuffer });

    const [, init] = fetchMock.mock.calls[0];
    const filePart = (init.body as FormData).get('file') as File;
    expect(filePart.size).toBe(3);
    expect(filePart.type).toBe('application/pdf');
  });

  it('returns the typed uploaded-file result with id, name, mimeType, size, and downloadUrl', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(uploadedFileResponse, 201));
    const files = buildResource(fetchMock);

    const result = await files.upload({
      fileName: 'invoice.pdf',
      mimeType: 'application/pdf',
      content: new Uint8Array([1, 2, 3]),
    });

    expect(result).toEqual(uploadedFileResponse);
  });
});
