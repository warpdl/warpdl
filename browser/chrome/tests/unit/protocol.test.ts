/**
 * Protocol unit tests.
 * Tests type guards, validation functions, and factory functions.
 */

import { describe, it, expect } from 'vitest';
import {
  isValidMethod,
  isNativeRequest,
  isNativeResponse,
  validateDownloadParams,
  validateResumeParams,
  validateStopParams,
  validateFlushParams,
  validateListParams,
  isVersionResponse,
  isDownloadResponse,
  isResumeResponse,
  isListResponse,
  isSuccessResponse,
  createRequest,
  getResponseError,
} from '@shared/protocol';

describe('isValidMethod', () => {
  it('returns true for valid methods', () => {
    expect(isValidMethod('version')).toBe(true);
    expect(isValidMethod('download')).toBe(true);
    expect(isValidMethod('list')).toBe(true);
    expect(isValidMethod('stop')).toBe(true);
    expect(isValidMethod('resume')).toBe(true);
    expect(isValidMethod('flush')).toBe(true);
  });

  it('returns false for invalid methods', () => {
    expect(isValidMethod('invalid')).toBe(false);
    expect(isValidMethod('')).toBe(false);
    expect(isValidMethod(null)).toBe(false);
    expect(isValidMethod(undefined)).toBe(false);
    expect(isValidMethod(123)).toBe(false);
    expect(isValidMethod({})).toBe(false);
  });
});

describe('isNativeRequest', () => {
  it('returns true for valid requests', () => {
    expect(isNativeRequest({ id: 1, method: 'version' })).toBe(true);
    expect(isNativeRequest({ id: 1, method: 'download', message: { url: 'https://example.com' } })).toBe(true);
    expect(isNativeRequest({ id: 0, method: 'list' })).toBe(true);
  });

  it('returns false for invalid requests', () => {
    expect(isNativeRequest(null)).toBe(false);
    expect(isNativeRequest(undefined)).toBe(false);
    expect(isNativeRequest({})).toBe(false);
    expect(isNativeRequest({ id: 'string', method: 'version' })).toBe(false);
    expect(isNativeRequest({ id: 1, method: 'invalid' })).toBe(false);
    expect(isNativeRequest({ id: 1 })).toBe(false);
    expect(isNativeRequest({ method: 'version' })).toBe(false);
    expect(isNativeRequest({ id: 1, method: 'version', message: 'not-object' })).toBe(false);
  });
});

describe('isNativeResponse', () => {
  it('returns true for valid responses', () => {
    expect(isNativeResponse({ id: 1, ok: true })).toBe(true);
    expect(isNativeResponse({ id: 1, ok: false, error: 'error message' })).toBe(true);
    expect(isNativeResponse({ id: 1, ok: true, result: { data: 'value' } })).toBe(true);
  });

  it('returns false for invalid responses', () => {
    expect(isNativeResponse(null)).toBe(false);
    expect(isNativeResponse(undefined)).toBe(false);
    expect(isNativeResponse({})).toBe(false);
    expect(isNativeResponse({ id: 'string', ok: true })).toBe(false);
    expect(isNativeResponse({ id: 1, ok: 'string' })).toBe(false);
    expect(isNativeResponse({ id: 1 })).toBe(false);
    expect(isNativeResponse({ ok: true })).toBe(false);
    expect(isNativeResponse({ id: 1, ok: false, error: 123 })).toBe(false);
  });
});

describe('validateDownloadParams', () => {
  it('accepts valid params', () => {
    expect(() => validateDownloadParams({ url: 'https://example.com' })).not.toThrow();
    expect(() =>
      validateDownloadParams({
        url: 'https://example.com',
        fileName: 'file.zip',
        downloadDirectory: '/downloads',
        headers: { 'User-Agent': 'test' },
        forceParts: true,
        maxConnections: 8,
        maxSegments: 16,
        overwrite: false,
        proxy: 'http://proxy:8080',
        timeout: 30,
        speedLimit: '1M',
      })
    ).not.toThrow();
  });

  it('rejects invalid params', () => {
    expect(() => validateDownloadParams(null)).toThrow('must be an object');
    expect(() => validateDownloadParams(undefined)).toThrow('must be an object');
    expect(() => validateDownloadParams({})).toThrow('url is required');
    expect(() => validateDownloadParams({ url: '' })).toThrow('url is required');
    expect(() => validateDownloadParams({ url: 123 })).toThrow('url is required');
    expect(() => validateDownloadParams({ url: 'https://x.com', fileName: 123 })).toThrow('fileName must be a string');
    expect(() => validateDownloadParams({ url: 'https://x.com', downloadDirectory: 123 })).toThrow('downloadDirectory must be a string');
    expect(() => validateDownloadParams({ url: 'https://x.com', headers: 'not-object' })).toThrow('headers must be an object');
    expect(() => validateDownloadParams({ url: 'https://x.com', headers: { key: 123 } })).toThrow('headers must be Record<string, string>');
    expect(() => validateDownloadParams({ url: 'https://x.com', forceParts: 'yes' })).toThrow('forceParts must be a boolean');
    expect(() => validateDownloadParams({ url: 'https://x.com', maxConnections: 'eight' })).toThrow('maxConnections must be a number');
    expect(() => validateDownloadParams({ url: 'https://x.com', maxSegments: 'sixteen' })).toThrow('maxSegments must be a number');
    expect(() => validateDownloadParams({ url: 'https://x.com', overwrite: 'no' })).toThrow('overwrite must be a boolean');
    expect(() => validateDownloadParams({ url: 'https://x.com', proxy: 123 })).toThrow('proxy must be a string');
    expect(() => validateDownloadParams({ url: 'https://x.com', timeout: 'thirty' })).toThrow('timeout must be a number');
    expect(() => validateDownloadParams({ url: 'https://x.com', speedLimit: 1000 })).toThrow('speedLimit must be a string');
  });
});

describe('validateResumeParams', () => {
  it('accepts valid params', () => {
    expect(() => validateResumeParams({ downloadId: 'dl-123' })).not.toThrow();
    expect(() =>
      validateResumeParams({
        downloadId: 'dl-123',
        headers: { Cookie: 'auth=token' },
        forceParts: true,
        maxConnections: 4,
        maxSegments: 8,
        proxy: 'socks5://proxy:1080',
        timeout: 60,
        speedLimit: '500K',
      })
    ).not.toThrow();
  });

  it('rejects invalid params', () => {
    expect(() => validateResumeParams(null)).toThrow('must be an object');
    expect(() => validateResumeParams({})).toThrow('downloadId is required');
    expect(() => validateResumeParams({ downloadId: '' })).toThrow('downloadId is required');
    expect(() => validateResumeParams({ downloadId: 123 })).toThrow('downloadId is required');
    expect(() => validateResumeParams({ downloadId: 'dl-1', headers: 'bad' })).toThrow('headers must be an object');
    expect(() => validateResumeParams({ downloadId: 'dl-1', forceParts: 1 })).toThrow('forceParts must be a boolean');
  });
});

describe('validateStopParams', () => {
  it('accepts valid params', () => {
    expect(() => validateStopParams({ downloadId: 'dl-123' })).not.toThrow();
  });

  it('rejects invalid params', () => {
    expect(() => validateStopParams(null)).toThrow('must be an object');
    expect(() => validateStopParams({})).toThrow('downloadId is required');
    expect(() => validateStopParams({ downloadId: '' })).toThrow('downloadId is required');
  });
});

describe('validateFlushParams', () => {
  it('accepts valid params', () => {
    expect(() => validateFlushParams({ downloadId: 'dl-123' })).not.toThrow();
  });

  it('rejects invalid params', () => {
    expect(() => validateFlushParams(null)).toThrow('must be an object');
    expect(() => validateFlushParams({})).toThrow('downloadId is required');
  });
});

describe('validateListParams', () => {
  it('accepts valid params', () => {
    expect(() => validateListParams(undefined)).not.toThrow();
    expect(() => validateListParams(null)).not.toThrow();
    expect(() => validateListParams({})).not.toThrow();
    expect(() => validateListParams({ includeHidden: true })).not.toThrow();
    expect(() => validateListParams({ includeMetadata: false })).not.toThrow();
    expect(() => validateListParams({ includeHidden: true, includeMetadata: true })).not.toThrow();
  });

  it('rejects invalid params', () => {
    expect(() => validateListParams('string')).toThrow('must be an object');
    expect(() => validateListParams({ includeHidden: 'yes' })).toThrow('includeHidden must be a boolean');
    expect(() => validateListParams({ includeMetadata: 1 })).toThrow('includeMetadata must be a boolean');
  });
});

describe('response type guards', () => {
  describe('isVersionResponse', () => {
    it('returns true for valid version responses', () => {
      expect(isVersionResponse({ version: '1.0.0' })).toBe(true);
      expect(isVersionResponse({ version: '1.0.0', commit: 'abc', buildDate: '2024-01-01' })).toBe(true);
    });

    it('returns false for invalid responses', () => {
      expect(isVersionResponse(null)).toBe(false);
      expect(isVersionResponse({})).toBe(false);
      expect(isVersionResponse({ version: 123 })).toBe(false);
    });
  });

  describe('isDownloadResponse', () => {
    it('returns true for valid download responses', () => {
      expect(isDownloadResponse({ downloadId: 'dl-1', fileName: 'file.zip', totalSize: 1024 })).toBe(true);
    });

    it('returns false for invalid responses', () => {
      expect(isDownloadResponse(null)).toBe(false);
      expect(isDownloadResponse({})).toBe(false);
      expect(isDownloadResponse({ downloadId: 'dl-1' })).toBe(false);
      expect(isDownloadResponse({ downloadId: 'dl-1', fileName: 'f' })).toBe(false);
      expect(isDownloadResponse({ downloadId: 123, fileName: 'f', totalSize: 1 })).toBe(false);
    });
  });

  describe('isResumeResponse', () => {
    it('returns true for valid resume responses', () => {
      expect(isResumeResponse({ downloadId: 'dl-1', fileName: 'file.zip' })).toBe(true);
    });

    it('returns false for invalid responses', () => {
      expect(isResumeResponse(null)).toBe(false);
      expect(isResumeResponse({ downloadId: 'dl-1' })).toBe(false);
    });
  });

  describe('isListResponse', () => {
    it('returns true for valid list responses', () => {
      expect(isListResponse({ downloads: [] })).toBe(true);
      expect(isListResponse({ downloads: [{ id: 'dl-1' }] })).toBe(true);
    });

    it('returns false for invalid responses', () => {
      expect(isListResponse(null)).toBe(false);
      expect(isListResponse({})).toBe(false);
      expect(isListResponse({ downloads: 'not-array' })).toBe(false);
    });
  });

  describe('isSuccessResponse', () => {
    it('returns true for valid success responses', () => {
      expect(isSuccessResponse({ success: true })).toBe(true);
      expect(isSuccessResponse({ success: false })).toBe(true);
    });

    it('returns false for invalid responses', () => {
      expect(isSuccessResponse(null)).toBe(false);
      expect(isSuccessResponse({})).toBe(false);
      expect(isSuccessResponse({ success: 'yes' })).toBe(false);
    });
  });
});

describe('createRequest', () => {
  it('creates request without message', () => {
    const req = createRequest(1, 'version');
    expect(req).toEqual({ id: 1, method: 'version' });
    expect('message' in req).toBe(false);
  });

  it('creates request with message', () => {
    const req = createRequest(2, 'download', { url: 'https://example.com' });
    expect(req).toEqual({ id: 2, method: 'download', message: { url: 'https://example.com' } });
  });
});

describe('getResponseError', () => {
  it('returns undefined for successful responses', () => {
    expect(getResponseError({ id: 1, ok: true })).toBeUndefined();
    expect(getResponseError({ id: 1, ok: true, result: {} })).toBeUndefined();
  });

  it('returns error message for failed responses', () => {
    expect(getResponseError({ id: 1, ok: false, error: 'connection failed' })).toBe('connection failed');
  });

  it('returns default error for failed responses without message', () => {
    expect(getResponseError({ id: 1, ok: false })).toBe('Unknown error');
  });
});
