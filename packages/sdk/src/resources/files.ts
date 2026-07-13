import type { HttpClient } from '../http.js';
import type { FilesApi, UploadedFile, UploadFileInput } from '../types.js';

const DEFAULT_MIME_TYPE = 'application/octet-stream';

// FilesResource uploads a file for later use as a file-typed tool input
// (PD22). It builds the multipart body itself from the platform's own
// global FormData/Blob (available since Node 18, the SDK's baseline —
// http.ts already relies on global fetch) rather than a third-party
// multipart library, keeping the SDK zero-dependency.
export class FilesResource implements FilesApi {
  constructor(private readonly http: HttpClient) {}

  upload(input: UploadFileInput): Promise<UploadedFile> {
    const form = new FormData();
    form.set('file', toBlob(input), input.fileName);
    return this.http.postMultipart<UploadedFile>('/api/v1/files', form);
  }
}

function toBlob(input: UploadFileInput): Blob {
  if (input.content instanceof Blob) {
    return input.mimeType ? new Blob([input.content], { type: input.mimeType }) : input.content;
  }
  // Re-wrapping in a fresh Uint8Array backed by a plain ArrayBuffer sidesteps
  // BlobPart's refusal to accept a Uint8Array typed over the wider
  // ArrayBufferLike (which also covers SharedArrayBuffer).
  const bytes = input.content instanceof Uint8Array ? new Uint8Array(input.content) : input.content;
  return new Blob([bytes], { type: input.mimeType ?? DEFAULT_MIME_TYPE });
}
