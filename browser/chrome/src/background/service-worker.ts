/**
 * Service Worker entry point for the WarpDL Chrome extension.
 * Initializes all extension components and handles lifecycle events.
 */

import { ALARM_NAMES, MESSAGE_TYPES } from '@shared/constants';
import { getClient } from './native-messaging';
import { getStateManager } from './state';
import { startInterceptor, stopInterceptor } from './download-interceptor';
import { initContextMenus } from './context-menu';

/**
 * Extension initialization state.
 */
let initialized = false;

/**
 * Initializes the extension components.
 */
async function initialize(): Promise<void> {
  if (initialized) {
    return;
  }

  console.log('[WarpDL] Initializing extension...');

  try {
    // Initialize state manager first
    const stateManager = getStateManager();
    await stateManager.getState();

    // Connect to native host
    const client = getClient();
    try {
      await client.connect();
      console.log('[WarpDL] Connected to daemon');
    } catch (err) {
      console.warn('[WarpDL] Failed to connect to daemon:', err);
      // Continue initialization even if daemon is not available
    }

    // Initialize context menus
    await initContextMenus();

    // Start download interceptor
    const state = await stateManager.getState();
    if (state.enabled) {
      startInterceptor();
    }

    // Set up keep-alive alarm for active downloads
    setupKeepAliveAlarm();

    // Listen for state changes to toggle interceptor
    stateManager.addListener((newState) => {
      if (newState.enabled) {
        startInterceptor();
      } else {
        stopInterceptor();
      }
    });

    initialized = true;
    console.log('[WarpDL] Extension initialized');
  } catch (err) {
    console.error('[WarpDL] Failed to initialize extension:', err);
    throw err;
  }
}

/**
 * Sets up a keep-alive alarm to prevent service worker termination
 * during active downloads.
 */
function setupKeepAliveAlarm(): void {
  // Create alarm that fires every 25 seconds (service worker can be terminated after 30s of inactivity)
  chrome.alarms.create(ALARM_NAMES.KEEP_ALIVE, {
    periodInMinutes: 0.4, // ~24 seconds
  });
}

/**
 * Clears the keep-alive alarm.
 */
async function clearKeepAliveAlarm(): Promise<void> {
  await chrome.alarms.clear(ALARM_NAMES.KEEP_ALIVE);
}

/**
 * Handles keep-alive alarm events.
 * Checks if there are active downloads and keeps connection alive.
 */
async function handleKeepAliveAlarm(): Promise<void> {
  const stateManager = getStateManager();
  const state = await stateManager.getState();

  if (state.activeDownloadCount > 0) {
    // Keep the connection alive by checking daemon status
    const client = getClient();
    if (client.isConnected()) {
      try {
        await client.version();
      } catch {
        // Ignore version check errors
      }
    } else {
      // Try to reconnect
      try {
        await client.connect();
      } catch {
        // Ignore reconnection errors
      }
    }
  }
}

/**
 * Handles messages from popup and options pages.
 * Note: Must return true synchronously to keep response channel open for async responses.
 */
function handleMessage(
  message: { type: string; payload?: unknown },
  _sender: chrome.runtime.MessageSender,
  sendResponse: (response?: unknown) => void
): boolean {
  switch (message.type) {
    case MESSAGE_TYPES.TOGGLE_ENABLED: {
      const stateManager = getStateManager();
      stateManager.toggleEnabled().then((newEnabled) => {
        sendResponse({ enabled: newEnabled });
      });
      return true; // Keep channel open for async response
    }

    case MESSAGE_TYPES.GET_STATE: {
      const stateManager = getStateManager();
      stateManager.getState().then((state) => {
        sendResponse(state);
      });
      return true; // Keep channel open for async response
    }

    default:
      return false;
  }
}

/**
 * Handles extension installation and updates.
 */
function handleInstalled(details: chrome.runtime.InstalledDetails): void {
  console.log('[WarpDL] Extension installed/updated:', details.reason);

  if (details.reason === 'install') {
    // First-time installation
    console.log('[WarpDL] First-time installation');
  } else if (details.reason === 'update') {
    console.log('[WarpDL] Extension updated from', details.previousVersion);
  }

  // Initialize on install/update
  initialize();
}

/**
 * Handles browser startup.
 */
function handleStartup(): void {
  console.log('[WarpDL] Browser startup');
  initialize();
}

/**
 * Handles alarm events.
 */
function handleAlarm(alarm: chrome.alarms.Alarm): void {
  if (alarm.name === ALARM_NAMES.KEEP_ALIVE) {
    handleKeepAliveAlarm();
  }
}

// Register event listeners
chrome.runtime.onInstalled.addListener(handleInstalled);
chrome.runtime.onStartup.addListener(handleStartup);
chrome.alarms.onAlarm.addListener(handleAlarm);
chrome.runtime.onMessage.addListener(handleMessage);

// Export for testing
export {
  initialize,
  handleMessage,
  handleInstalled,
  handleStartup,
  handleAlarm,
  setupKeepAliveAlarm,
  clearKeepAliveAlarm,
};
