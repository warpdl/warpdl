/**
 * Protocol utilities for native messaging communication.
 * Provides type guards, validation, and factory functions.
 */

import type {
  NativeRequest,
  NativeResponse,
  NativeMethod,
  DownloadParams,
  ResumeParams,
  StopParams,
  FlushParams,
  ListParams,
  VersionResponse,
  DownloadResponse,
  ResumeResponse,
  ListResponse,
  SuccessResponse,
} from './types';

/**
 * Valid native host methods.
 */
const VALID_METHODS: readonly NativeMethod[] = [
  'version',
  'download',
  'list',
  'stop',
  'resume',
  'flush',
] as const;

/**
 * Type guard for NativeMethod.
 */
export function isValidMethod(method: unknown): method is NativeMethod {
  return typeof method === 'string' && VALID_METHODS.includes(method as NativeMethod);
}

/**
 * Type guard for NativeRequest.
 */
export function isNativeRequest(obj: unknown): obj is NativeRequest {
  if (typeof obj !== 'object' || obj === null) return false;
  const req = obj as Record<string, unknown>;
  return (
    typeof req['id'] === 'number' &&
    isValidMethod(req['method']) &&
    (req['message'] === undefined || (typeof req['message'] === 'object' && req['message'] !== null))
  );
}

/**
 * Type guard for NativeResponse.
 */
export function isNativeResponse(obj: unknown): obj is NativeResponse {
  if (typeof obj !== 'object' || obj === null) return false;
  const res = obj as Record<string, unknown>;
  return (
    typeof res['id'] === 'number' &&
    typeof res['ok'] === 'boolean' &&
    (res['error'] === undefined || typeof res['error'] === 'string')
  );
}

/**
 * Validates DownloadParams.
 * @throws Error if validation fails
 */
export function validateDownloadParams(params: unknown): asserts params is DownloadParams {
  if (typeof params !== 'object' || params === null) {
    throw new Error('download params must be an object');
  }
  const p = params as Record<string, unknown>;
  if (typeof p['url'] !== 'string' || p['url'].length === 0) {
    throw new Error('url is required and must be a non-empty string');
  }
  if (p['fileName'] !== undefined && typeof p['fileName'] !== 'string') {
    throw new Error('fileName must be a string');
  }
  if (p['downloadDirectory'] !== undefined && typeof p['downloadDirectory'] !== 'string') {
    throw new Error('downloadDirectory must be a string');
  }
  if (p['headers'] !== undefined) {
    if (typeof p['headers'] !== 'object' || p['headers'] === null) {
      throw new Error('headers must be an object');
    }
    for (const [key, value] of Object.entries(p['headers'] as object)) {
      if (typeof key !== 'string' || typeof value !== 'string') {
        throw new Error('headers must be Record<string, string>');
      }
    }
  }
  if (p['forceParts'] !== undefined && typeof p['forceParts'] !== 'boolean') {
    throw new Error('forceParts must be a boolean');
  }
  if (p['maxConnections'] !== undefined && typeof p['maxConnections'] !== 'number') {
    throw new Error('maxConnections must be a number');
  }
  if (p['maxSegments'] !== undefined && typeof p['maxSegments'] !== 'number') {
    throw new Error('maxSegments must be a number');
  }
  if (p['overwrite'] !== undefined && typeof p['overwrite'] !== 'boolean') {
    throw new Error('overwrite must be a boolean');
  }
  if (p['proxy'] !== undefined && typeof p['proxy'] !== 'string') {
    throw new Error('proxy must be a string');
  }
  if (p['timeout'] !== undefined && typeof p['timeout'] !== 'number') {
    throw new Error('timeout must be a number');
  }
  if (p['speedLimit'] !== undefined && typeof p['speedLimit'] !== 'string') {
    throw new Error('speedLimit must be a string');
  }
}

/**
 * Validates ResumeParams.
 * @throws Error if validation fails
 */
export function validateResumeParams(params: unknown): asserts params is ResumeParams {
  if (typeof params !== 'object' || params === null) {
    throw new Error('resume params must be an object');
  }
  const p = params as Record<string, unknown>;
  if (typeof p['downloadId'] !== 'string' || p['downloadId'].length === 0) {
    throw new Error('downloadId is required and must be a non-empty string');
  }
  // Optional fields use same validation as DownloadParams
  if (p['headers'] !== undefined) {
    if (typeof p['headers'] !== 'object' || p['headers'] === null) {
      throw new Error('headers must be an object');
    }
  }
  if (p['forceParts'] !== undefined && typeof p['forceParts'] !== 'boolean') {
    throw new Error('forceParts must be a boolean');
  }
  if (p['maxConnections'] !== undefined && typeof p['maxConnections'] !== 'number') {
    throw new Error('maxConnections must be a number');
  }
  if (p['maxSegments'] !== undefined && typeof p['maxSegments'] !== 'number') {
    throw new Error('maxSegments must be a number');
  }
  if (p['proxy'] !== undefined && typeof p['proxy'] !== 'string') {
    throw new Error('proxy must be a string');
  }
  if (p['timeout'] !== undefined && typeof p['timeout'] !== 'number') {
    throw new Error('timeout must be a number');
  }
  if (p['speedLimit'] !== undefined && typeof p['speedLimit'] !== 'string') {
    throw new Error('speedLimit must be a string');
  }
}

/**
 * Validates StopParams.
 * @throws Error if validation fails
 */
export function validateStopParams(params: unknown): asserts params is StopParams {
  if (typeof params !== 'object' || params === null) {
    throw new Error('stop params must be an object');
  }
  const p = params as Record<string, unknown>;
  if (typeof p['downloadId'] !== 'string' || p['downloadId'].length === 0) {
    throw new Error('downloadId is required and must be a non-empty string');
  }
}

/**
 * Validates FlushParams.
 * @throws Error if validation fails
 */
export function validateFlushParams(params: unknown): asserts params is FlushParams {
  if (typeof params !== 'object' || params === null) {
    throw new Error('flush params must be an object');
  }
  const p = params as Record<string, unknown>;
  if (typeof p['downloadId'] !== 'string' || p['downloadId'].length === 0) {
    throw new Error('downloadId is required and must be a non-empty string');
  }
}

/**
 * Validates ListParams.
 * @throws Error if validation fails
 */
export function validateListParams(params: unknown): asserts params is ListParams {
  if (params === undefined || params === null) {
    return; // All fields are optional
  }
  if (typeof params !== 'object') {
    throw new Error('list params must be an object');
  }
  const p = params as Record<string, unknown>;
  if (p['includeHidden'] !== undefined && typeof p['includeHidden'] !== 'boolean') {
    throw new Error('includeHidden must be a boolean');
  }
  if (p['includeMetadata'] !== undefined && typeof p['includeMetadata'] !== 'boolean') {
    throw new Error('includeMetadata must be a boolean');
  }
}

/**
 * Type guard for VersionResponse.
 */
export function isVersionResponse(obj: unknown): obj is VersionResponse {
  if (typeof obj !== 'object' || obj === null) return false;
  const res = obj as Record<string, unknown>;
  return typeof res['version'] === 'string';
}

/**
 * Type guard for DownloadResponse.
 */
export function isDownloadResponse(obj: unknown): obj is DownloadResponse {
  if (typeof obj !== 'object' || obj === null) return false;
  const res = obj as Record<string, unknown>;
  return (
    typeof res['downloadId'] === 'string' &&
    typeof res['fileName'] === 'string' &&
    typeof res['totalSize'] === 'number'
  );
}

/**
 * Type guard for ResumeResponse.
 */
export function isResumeResponse(obj: unknown): obj is ResumeResponse {
  if (typeof obj !== 'object' || obj === null) return false;
  const res = obj as Record<string, unknown>;
  return typeof res['downloadId'] === 'string' && typeof res['fileName'] === 'string';
}

/**
 * Type guard for ListResponse.
 */
export function isListResponse(obj: unknown): obj is ListResponse {
  if (typeof obj !== 'object' || obj === null) return false;
  const res = obj as Record<string, unknown>;
  return Array.isArray(res['downloads']);
}

/**
 * Type guard for SuccessResponse.
 */
export function isSuccessResponse(obj: unknown): obj is SuccessResponse {
  if (typeof obj !== 'object' || obj === null) return false;
  const res = obj as Record<string, unknown>;
  return typeof res['success'] === 'boolean';
}

/**
 * Creates a NativeRequest object.
 */
export function createRequest(
  id: number,
  method: NativeMethod,
  message?: Record<string, unknown>
): NativeRequest {
  const request: NativeRequest = { id, method };
  if (message !== undefined) {
    request.message = message;
  }
  return request;
}

/**
 * Extracts error message from a NativeResponse.
 * Returns undefined if the response indicates success.
 */
export function getResponseError(response: NativeResponse): string | undefined {
  if (response.ok) return undefined;
  return response.error ?? 'Unknown error';
}
