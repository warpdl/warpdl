/**
 * Popup UI logic.
 * Displays extension status and provides toggle controls.
 */

import { MESSAGE_TYPES } from '@shared/constants';
import type { ExtensionState, StateChangePayload, ExtensionMessage } from '@shared/types';

/**
 * UI element references.
 */
interface UIElements {
  connectionStatus: HTMLElement;
  downloadCount: HTMLElement;
  enableToggle: HTMLInputElement;
  optionsLink: HTMLAnchorElement;
}

/**
 * Gets UI element references.
 */
function getElements(): UIElements {
  return {
    connectionStatus: document.getElementById('connection-status')!,
    downloadCount: document.getElementById('download-count')!,
    enableToggle: document.getElementById('enable-toggle') as HTMLInputElement,
    optionsLink: document.getElementById('options-link') as HTMLAnchorElement,
  };
}

/**
 * Updates the connection status display.
 */
function updateConnectionStatus(elements: UIElements, connected: boolean): void {
  const { connectionStatus } = elements;
  connectionStatus.textContent = connected ? 'Connected' : 'Disconnected';
  connectionStatus.className = `status-badge ${connected ? 'connected' : 'disconnected'}`;
}

/**
 * Updates the download count display.
 */
function updateDownloadCount(elements: UIElements, count: number): void {
  elements.downloadCount.textContent = String(count);
}

/**
 * Updates the enable toggle state.
 */
function updateEnableToggle(elements: UIElements, enabled: boolean): void {
  elements.enableToggle.checked = enabled;
}

/**
 * Updates all UI elements based on state.
 */
function updateUI(elements: UIElements, state: ExtensionState): void {
  updateConnectionStatus(elements, state.connected);
  updateDownloadCount(elements, state.activeDownloadCount);
  updateEnableToggle(elements, state.enabled);
}

/**
 * Handles state change messages from the service worker.
 */
function handleStateChange(elements: UIElements, payload: StateChangePayload): void {
  if (payload.connected !== undefined) {
    updateConnectionStatus(elements, payload.connected);
  }
  if (payload.activeDownloadCount !== undefined) {
    updateDownloadCount(elements, payload.activeDownloadCount);
  }
  if (payload.enabled !== undefined) {
    updateEnableToggle(elements, payload.enabled);
  }
}

/**
 * Loads the current state from the service worker.
 */
async function loadState(): Promise<ExtensionState | null> {
  try {
    const response = await chrome.runtime.sendMessage({
      type: MESSAGE_TYPES.GET_STATE,
    });
    return response as ExtensionState;
  } catch (err) {
    console.error('[WarpDL Popup] Failed to load state:', err);
    return null;
  }
}

/**
 * Toggles the enabled state.
 */
async function toggleEnabled(): Promise<void> {
  try {
    await chrome.runtime.sendMessage({
      type: MESSAGE_TYPES.TOGGLE_ENABLED,
    });
  } catch (err) {
    console.error('[WarpDL Popup] Failed to toggle enabled:', err);
  }
}

/**
 * Opens the options page.
 */
function openOptions(): void {
  chrome.runtime.openOptionsPage();
}

/**
 * Initializes the popup.
 */
async function init(): Promise<void> {
  const elements = getElements();

  // Load initial state
  const state = await loadState();
  if (state) {
    updateUI(elements, state);
  }

  // Set up toggle handler
  elements.enableToggle.addEventListener('change', async (e) => {
    e.preventDefault();
    await toggleEnabled();
  });

  // Set up options link
  elements.optionsLink.addEventListener('click', (e) => {
    e.preventDefault();
    openOptions();
  });

  // Listen for state changes
  chrome.runtime.onMessage.addListener((message: ExtensionMessage) => {
    if (message.type === MESSAGE_TYPES.STATE_CHANGED) {
      handleStateChange(elements, message.payload as StateChangePayload);
    }
  });
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', init);
