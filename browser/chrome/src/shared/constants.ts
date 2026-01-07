/**
 * Native messaging host name registered with the browser.
 * Must match the name in the native host manifest.
 */
export const NATIVE_HOST_NAME = 'com.warpdl.host';

/**
 * Default request timeout in milliseconds.
 */
export const DEFAULT_TIMEOUT_MS = 30_000;

/**
 * Maximum reconnection attempts before giving up.
 */
export const MAX_RECONNECT_ATTEMPTS = 3;

/**
 * Base delay for exponential backoff reconnection (ms).
 */
export const RECONNECT_BASE_DELAY_MS = 1_000;

/**
 * Default file size threshold for download interception (bytes).
 * Downloads larger than this will be redirected to WarpDL.
 */
export const DEFAULT_SIZE_THRESHOLD_BYTES = 1024 * 1024; // 1MB

/**
 * Default maximum concurrent connections per download.
 */
export const DEFAULT_MAX_CONNECTIONS = 8;

/**
 * Default maximum segments per download.
 */
export const DEFAULT_MAX_SEGMENTS = 16;

/**
 * Storage keys for chrome.storage.sync (persistent settings).
 */
export const STORAGE_KEYS = {
  /** Whether download interception is enabled */
  ENABLED: 'enabled',
  /** File size threshold in bytes */
  SIZE_THRESHOLD: 'sizeThreshold',
  /** List of excluded domains */
  EXCLUDED_DOMAINS: 'excludedDomains',
  /** Default download directory */
  DOWNLOAD_DIRECTORY: 'downloadDirectory',
  /** Maximum connections per download */
  MAX_CONNECTIONS: 'maxConnections',
  /** Maximum segments per download */
  MAX_SEGMENTS: 'maxSegments',
} as const;

/**
 * Session storage keys for chrome.storage.session (ephemeral state).
 */
export const SESSION_KEYS = {
  /** Whether connected to native host */
  CONNECTED: 'connected',
  /** Map of active download IDs */
  ACTIVE_DOWNLOADS: 'activeDownloads',
} as const;

/**
 * Context menu IDs.
 */
export const CONTEXT_MENU_IDS = {
  DOWNLOAD_LINK: 'warpdl-download-link',
  DOWNLOAD_IMAGE: 'warpdl-download-image',
  DOWNLOAD_VIDEO: 'warpdl-download-video',
  DOWNLOAD_AUDIO: 'warpdl-download-audio',
} as const;

/**
 * Alarm names for service worker keep-alive.
 */
export const ALARM_NAMES = {
  KEEP_ALIVE: 'warpdl-keep-alive',
} as const;

/**
 * Message types for internal extension messaging.
 */
export const MESSAGE_TYPES = {
  /** Toggle enabled state */
  TOGGLE_ENABLED: 'toggleEnabled',
  /** State changed notification */
  STATE_CHANGED: 'stateChanged',
  /** Download started */
  DOWNLOAD_STARTED: 'downloadStarted',
  /** Download completed */
  DOWNLOAD_COMPLETED: 'downloadCompleted',
  /** Download failed */
  DOWNLOAD_FAILED: 'downloadFailed',
  /** Get current state */
  GET_STATE: 'getState',
} as const;
