/**
 * Download Interceptor for intercepting browser downloads.
 * Uses chrome.downloads.onDeterminingFilename to intercept downloads
 * above a size threshold and redirect them to the WarpDL daemon.
 */

import { getClient } from './native-messaging';
import { getStateManager } from './state';
import { loadSettings, isDomainExcluded, isAboveThreshold } from '@shared/storage';
import type { DownloadParams } from '@shared/types';

/**
 * Filename suggestion callback type.
 */
type FilenameSuggestionCallback = (suggestion?: {
  filename: string;
  conflictAction?: chrome.downloads.FilenameConflictAction;
}) => void;

/**
 * Listener function type for onDeterminingFilename.
 */
type DeterminingFilenameListener = (
  downloadItem: chrome.downloads.DownloadItem,
  suggest: FilenameSuggestionCallback
) => void;

/**
 * State for the download interceptor.
 */
interface InterceptorState {
  enabled: boolean;
  listener: DeterminingFilenameListener | null;
}

const state: InterceptorState = {
  enabled: false,
  listener: null,
};

/**
 * Extracts domain from a URL.
 */
function extractDomain(url: string): string {
  try {
    const urlObj = new URL(url);
    return urlObj.hostname;
  } catch {
    return '';
  }
}

/**
 * Extracts filename from a URL or download item.
 */
function extractFilename(downloadItem: chrome.downloads.DownloadItem): string {
  // Use the suggested filename if available
  if (downloadItem.filename) {
    // Extract just the filename from the full path
    const parts = downloadItem.filename.split(/[/\\]/);
    return parts[parts.length - 1] ?? downloadItem.filename;
  }

  // Try to extract from URL
  try {
    const urlObj = new URL(downloadItem.finalUrl || downloadItem.url);
    const pathParts = urlObj.pathname.split('/');
    const lastPart = pathParts[pathParts.length - 1];
    if (lastPart && lastPart.length > 0 && lastPart !== '/') {
      return decodeURIComponent(lastPart);
    }
  } catch {
    // Ignore URL parsing errors
  }

  return 'download';
}

/**
 * Checks if a download should be intercepted.
 */
async function shouldIntercept(downloadItem: chrome.downloads.DownloadItem): Promise<boolean> {
  const settings = await loadSettings();

  // Check if interception is enabled
  if (!settings.enabled) {
    return false;
  }

  // Check domain exclusion
  const domain = extractDomain(downloadItem.finalUrl || downloadItem.url);
  if (domain && await isDomainExcluded(domain)) {
    return false;
  }

  // Check file size threshold (fileSize may be -1 if unknown)
  if (downloadItem.fileSize > 0 && !await isAboveThreshold(downloadItem.fileSize)) {
    return false;
  }

  // If size is unknown, intercept by default (can be configured)
  if (downloadItem.fileSize <= 0) {
    // For unknown sizes, intercept if enabled
    return true;
  }

  return true;
}

/**
 * Creates download parameters from a Chrome download item.
 */
async function createDownloadParams(
  downloadItem: chrome.downloads.DownloadItem
): Promise<DownloadParams> {
  const settings = await loadSettings();

  const params: DownloadParams = {
    url: downloadItem.finalUrl || downloadItem.url,
    fileName: extractFilename(downloadItem),
    maxConnections: settings.maxConnections,
    maxSegments: settings.maxSegments,
  };

  if (settings.downloadDirectory) {
    params.downloadDirectory = settings.downloadDirectory;
  }

  if (downloadItem.referrer) {
    params.headers = { Referer: downloadItem.referrer };
  }

  return params;
}

/**
 * Handles a download interception attempt.
 * Returns true if the download was successfully intercepted.
 */
async function handleDownload(
  downloadItem: chrome.downloads.DownloadItem,
  _suggest: FilenameSuggestionCallback
): Promise<boolean> {
  try {
    const client = getClient();

    // Check if connected to native host
    if (!client.isConnected()) {
      console.log('[WarpDL] Not connected to daemon, falling back to browser');
      return false;
    }

    // Create and send download request
    const params = await createDownloadParams(downloadItem);
    const response = await client.download(params);

    // Track the download
    const stateManager = getStateManager();
    await stateManager.trackDownload(response.downloadId);

    console.log(`[WarpDL] Download intercepted: ${params.fileName} (${response.downloadId})`);

    // Cancel the Chrome download
    await chrome.downloads.cancel(downloadItem.id);

    return true;
  } catch (err) {
    console.error('[WarpDL] Failed to intercept download:', err);
    return false;
  }
}

/**
 * The main download interception listener.
 * Returns true to indicate async handling (required for Chrome API).
 */
const downloadListener = (
  downloadItem: chrome.downloads.DownloadItem,
  suggest: FilenameSuggestionCallback
): boolean => {
  console.log('[WarpDL] Download detected:', downloadItem.url, 'Size:', downloadItem.fileSize);

  // Use async IIFE to handle async operations
  (async () => {
    // Check if we should intercept this download
    const shouldInterceptResult = await shouldIntercept(downloadItem);
    console.log('[WarpDL] Should intercept:', shouldInterceptResult);

    if (!shouldInterceptResult) {
      // Let the browser handle it normally
      suggest();
      return;
    }

    // Try to intercept
    const intercepted = await handleDownload(downloadItem, suggest);
    console.log('[WarpDL] Intercepted:', intercepted);

    if (!intercepted) {
      // Fallback: let browser handle it
      suggest();
    }
    // If intercepted, we already cancelled the download, no need to suggest
  })();

  // Return true to indicate we will call suggest() asynchronously
  return true;
};

/**
 * Listener for onCreated - fires when any download starts.
 * Used as fallback/debugging for onDeterminingFilename.
 */
const onCreatedListener = (downloadItem: chrome.downloads.DownloadItem): void => {
  console.log('[WarpDL] onCreated fired:', {
    id: downloadItem.id,
    url: downloadItem.url,
    filename: downloadItem.filename,
    fileSize: downloadItem.fileSize,
    state: downloadItem.state,
  });

  // Try to intercept using onCreated as fallback
  (async () => {
    const shouldInterceptResult = await shouldIntercept(downloadItem);
    console.log('[WarpDL] onCreated - Should intercept:', shouldInterceptResult);

    if (shouldInterceptResult) {
      try {
        const client = getClient();
        if (!client.isConnected()) {
          console.log('[WarpDL] Not connected to daemon');
          return;
        }

        const params = await createDownloadParams(downloadItem);
        console.log('[WarpDL] Sending to daemon:', params);

        const response = await client.download(params);
        console.log('[WarpDL] Daemon response:', response);

        // Track and cancel Chrome download
        const stateManager = getStateManager();
        await stateManager.trackDownload(response.downloadId);

        await chrome.downloads.cancel(downloadItem.id);
        console.log('[WarpDL] Chrome download cancelled, handled by WarpDL');
      } catch (err) {
        console.error('[WarpDL] Failed to intercept via onCreated:', err);
      }
    }
  })();
};

/**
 * Starts the download interceptor.
 */
export function startInterceptor(): void {
  if (state.enabled) {
    return;
  }

  // Add onDeterminingFilename listener
  state.listener = downloadListener;
  chrome.downloads.onDeterminingFilename.addListener(downloadListener);

  // Also add onCreated as fallback
  chrome.downloads.onCreated.addListener(onCreatedListener);

  state.enabled = true;

  console.log('[WarpDL] Download interceptor started');
  console.log('[WarpDL] Listeners registered:', {
    onDeterminingFilename: !!chrome.downloads.onDeterminingFilename,
    onCreated: !!chrome.downloads.onCreated,
  });
}

/**
 * Stops the download interceptor.
 */
export function stopInterceptor(): void {
  if (!state.enabled || !state.listener) {
    return;
  }

  chrome.downloads.onDeterminingFilename.removeListener(state.listener);
  chrome.downloads.onCreated.removeListener(onCreatedListener);
  state.listener = null;
  state.enabled = false;

  console.log('[WarpDL] Download interceptor stopped');
}

/**
 * Checks if the interceptor is currently enabled.
 */
export function isInterceptorEnabled(): boolean {
  return state.enabled;
}

/**
 * Manually triggers a download via WarpDL.
 * Useful for context menu downloads.
 */
export async function downloadWithWarpDL(
  url: string,
  filename?: string,
  referrer?: string
): Promise<string> {
  const client = getClient();

  if (!client.isConnected()) {
    throw new Error('Not connected to WarpDL daemon');
  }

  const settings = await loadSettings();

  const params: DownloadParams = {
    url,
    maxConnections: settings.maxConnections,
    maxSegments: settings.maxSegments,
  };

  if (filename) {
    params.fileName = filename;
  }

  if (settings.downloadDirectory) {
    params.downloadDirectory = settings.downloadDirectory;
  }

  if (referrer) {
    params.headers = { Referer: referrer };
  }

  const response = await client.download(params);

  // Track the download
  const stateManager = getStateManager();
  await stateManager.trackDownload(response.downloadId);

  return response.downloadId;
}
