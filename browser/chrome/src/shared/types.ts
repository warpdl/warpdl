/**
 * Native messaging protocol types.
 * These must match the Go structs in internal/nativehost/protocol.go and host.go exactly.
 */

/**
 * Native messaging request format.
 * Sent from extension to native host.
 */
export interface NativeRequest {
  /** Unique request ID for response correlation */
  id: number;
  /** Method to invoke on the native host */
  method: NativeMethod;
  /** Optional method parameters */
  message?: Record<string, unknown>;
}

/**
 * Native messaging response format.
 * Received from native host.
 */
export interface NativeResponse {
  /** Request ID this response correlates to */
  id: number;
  /** Whether the request succeeded */
  ok: boolean;
  /** Error message if ok is false */
  error?: string;
  /** Result data if ok is true */
  result?: unknown;
}

/**
 * Available native host methods.
 */
export type NativeMethod = 'version' | 'download' | 'list' | 'stop' | 'resume' | 'flush';

/**
 * Parameters for the download method.
 * Matches DownloadParams in internal/nativehost/host.go:54-67
 */
export interface DownloadParams {
  /** Download URL (required) */
  url: string;
  /** Optional filename override */
  fileName?: string;
  /** Optional download directory */
  downloadDirectory?: string;
  /** Optional HTTP headers */
  headers?: Record<string, string>;
  /** Force multi-part download even for small files */
  forceParts?: boolean;
  /** Maximum concurrent connections (int32) */
  maxConnections?: number;
  /** Maximum download segments (int32) */
  maxSegments?: number;
  /** Overwrite existing file */
  overwrite?: boolean;
  /** Proxy URL */
  proxy?: string;
  /** Request timeout in seconds */
  timeout?: number;
  /** Speed limit (e.g., "1M", "500K") */
  speedLimit?: string;
}

/**
 * Parameters for the resume method.
 * Matches ResumeParams in internal/nativehost/host.go:69-79
 */
export interface ResumeParams {
  /** Download ID to resume (required) */
  downloadId: string;
  /** Optional HTTP headers */
  headers?: Record<string, string>;
  /** Force multi-part download */
  forceParts?: boolean;
  /** Maximum concurrent connections */
  maxConnections?: number;
  /** Maximum download segments */
  maxSegments?: number;
  /** Proxy URL */
  proxy?: string;
  /** Request timeout in seconds */
  timeout?: number;
  /** Speed limit */
  speedLimit?: string;
}

/**
 * Parameters for the stop method.
 * Matches StopParams in internal/nativehost/host.go:81-83
 */
export interface StopParams {
  /** Download ID to stop (required) */
  downloadId: string;
}

/**
 * Parameters for the flush method.
 * Matches FlushParams in internal/nativehost/host.go:85-88
 */
export interface FlushParams {
  /** Download ID to flush (required) */
  downloadId: string;
}

/**
 * Parameters for the list method.
 * Matches ListParams in internal/nativehost/host.go:91-95
 */
export interface ListParams {
  /** Include hidden downloads */
  includeHidden?: boolean;
  /** Include full metadata */
  includeMetadata?: boolean;
}

/**
 * Version response from the daemon.
 */
export interface VersionResponse {
  version: string;
  commit?: string;
  buildDate?: string;
}

/**
 * Download response from the daemon.
 */
export interface DownloadResponse {
  downloadId: string;
  fileName: string;
  totalSize: number;
}

/**
 * Resume response from the daemon.
 */
export interface ResumeResponse {
  downloadId: string;
  fileName: string;
}

/**
 * List response from the daemon.
 */
export interface ListResponse {
  downloads: DownloadItem[];
}

/**
 * Individual download item in list response.
 */
export interface DownloadItem {
  id: string;
  url: string;
  fileName: string;
  downloadDirectory: string;
  totalSize: number;
  downloadedSize: number;
  status: DownloadStatus;
  createdAt: string;
  updatedAt: string;
}

/**
 * Download status values.
 */
export type DownloadStatus = 'pending' | 'downloading' | 'paused' | 'completed' | 'failed';

/**
 * Success response for stop/flush operations.
 */
export interface SuccessResponse {
  success: boolean;
}

/**
 * Extension settings stored in chrome.storage.sync.
 */
export interface ExtensionSettings {
  /** Whether download interception is enabled */
  enabled: boolean;
  /** File size threshold in bytes for interception */
  sizeThreshold: number;
  /** Domains to exclude from interception */
  excludedDomains: string[];
  /** Default download directory */
  downloadDirectory: string;
  /** Maximum connections per download */
  maxConnections: number;
  /** Maximum segments per download */
  maxSegments: number;
}

/**
 * Extension session state stored in chrome.storage.session.
 */
export interface ExtensionSessionState {
  /** Whether connected to native host */
  connected: boolean;
  /** Active download IDs being managed */
  activeDownloads: string[];
}

/**
 * Internal message format for extension messaging.
 */
export interface ExtensionMessage {
  type: string;
  payload?: unknown;
}

/**
 * State change notification payload.
 */
export interface StateChangePayload {
  connected?: boolean;
  enabled?: boolean;
  activeDownloadCount?: number;
}

/**
 * Combined extension state (used by popup and state manager).
 */
export interface ExtensionState {
  /** Whether download interception is enabled */
  enabled: boolean;
  /** Whether connected to native host */
  connected: boolean;
  /** Number of active downloads */
  activeDownloadCount: number;
  /** Full settings */
  settings: ExtensionSettings;
}
