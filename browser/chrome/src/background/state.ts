/**
 * State management for the extension.
 * Coordinates between native messaging client and storage,
 * and broadcasts state changes to popup/options pages.
 */

import { MESSAGE_TYPES } from '@shared/constants';
import {
  loadSettings,
  saveSettings,
  loadSessionState,
  saveSessionState,
} from '@shared/storage';
import type {
  ExtensionSettings,
  ExtensionSessionState,
  StateChangePayload,
  ExtensionState,
} from '@shared/types';
import { getClient, type ConnectionState, type ConnectionStateListener } from './native-messaging';

// Re-export ExtensionState for consumers
export type { ExtensionState };

/**
 * State change listener type.
 */
export type StateListener = (state: ExtensionState) => void;

/**
 * State manager singleton.
 */
class StateManager {
  private settings: ExtensionSettings | null = null;
  private sessionState: ExtensionSessionState | null = null;
  private listeners: Set<StateListener> = new Set();
  private connectionListener: ConnectionStateListener | null = null;
  private initialized = false;

  /**
   * Initializes the state manager.
   * Loads settings and session state from storage.
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    // Load initial state
    this.settings = await loadSettings();
    this.sessionState = await loadSessionState();

    // Listen to connection state changes
    const client = getClient();
    this.connectionListener = (state: ConnectionState) => {
      this.handleConnectionChange(state === 'connected');
    };
    client.addStateListener(this.connectionListener);

    // Listen to storage changes
    chrome.storage.onChanged.addListener(this.handleStorageChange);

    this.initialized = true;
  }

  /**
   * Gets the current state.
   */
  async getState(): Promise<ExtensionState> {
    if (!this.initialized) {
      await this.initialize();
    }

    return {
      enabled: this.settings!.enabled,
      connected: this.sessionState!.connected,
      activeDownloadCount: this.sessionState!.activeDownloads.length,
      settings: { ...this.settings! },
    };
  }

  /**
   * Gets cached settings synchronously.
   * Must call initialize() first.
   */
  getSettings(): ExtensionSettings | null {
    return this.settings ? { ...this.settings } : null;
  }

  /**
   * Gets cached session state synchronously.
   * Must call initialize() first.
   */
  getSessionState(): ExtensionSessionState | null {
    return this.sessionState ? { ...this.sessionState } : null;
  }

  /**
   * Toggles the enabled state.
   */
  async toggleEnabled(): Promise<boolean> {
    if (!this.initialized) {
      await this.initialize();
    }

    const newEnabled = !this.settings!.enabled;
    this.settings!.enabled = newEnabled;
    await saveSettings({ enabled: newEnabled });
    this.notifyListeners();
    this.broadcastStateChange({ enabled: newEnabled });

    return newEnabled;
  }

  /**
   * Sets the enabled state.
   */
  async setEnabled(enabled: boolean): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }

    if (this.settings!.enabled !== enabled) {
      this.settings!.enabled = enabled;
      await saveSettings({ enabled });
      this.notifyListeners();
      this.broadcastStateChange({ enabled });
    }
  }

  /**
   * Updates settings.
   */
  async updateSettings(newSettings: Partial<ExtensionSettings>): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }

    this.settings = { ...this.settings!, ...newSettings };
    await saveSettings(newSettings);
    this.notifyListeners();
  }

  /**
   * Adds a download to the active list.
   */
  async trackDownload(downloadId: string): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }

    if (!this.sessionState!.activeDownloads.includes(downloadId)) {
      this.sessionState!.activeDownloads.push(downloadId);
      await saveSessionState({ activeDownloads: this.sessionState!.activeDownloads });
      this.notifyListeners();
      this.broadcastStateChange({ activeDownloadCount: this.sessionState!.activeDownloads.length });
    }
  }

  /**
   * Removes a download from the active list.
   */
  async untrackDownload(downloadId: string): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }

    const index = this.sessionState!.activeDownloads.indexOf(downloadId);
    if (index !== -1) {
      this.sessionState!.activeDownloads.splice(index, 1);
      await saveSessionState({ activeDownloads: this.sessionState!.activeDownloads });
      this.notifyListeners();
      this.broadcastStateChange({ activeDownloadCount: this.sessionState!.activeDownloads.length });
    }
  }

  /**
   * Adds a state listener.
   */
  addListener(listener: StateListener): void {
    this.listeners.add(listener);
  }

  /**
   * Removes a state listener.
   */
  removeListener(listener: StateListener): void {
    this.listeners.delete(listener);
  }

  /**
   * Handles connection state changes.
   */
  private async handleConnectionChange(connected: boolean): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }

    if (this.sessionState!.connected !== connected) {
      this.sessionState!.connected = connected;
      await saveSessionState({ connected });
      this.notifyListeners();
      this.broadcastStateChange({ connected });
    }
  }

  /**
   * Handles storage changes from other contexts (popup, options).
   */
  private handleStorageChange = (
    changes: { [key: string]: chrome.storage.StorageChange },
    areaName: string
  ): void => {
    if (areaName === 'sync' && this.settings) {
      // Update local cache from storage changes
      let changed = false;
      for (const [key, change] of Object.entries(changes)) {
        if (key === 'enabled' && change.newValue !== undefined) {
          this.settings.enabled = change.newValue as boolean;
          changed = true;
        }
        if (key === 'sizeThreshold' && change.newValue !== undefined) {
          this.settings.sizeThreshold = change.newValue as number;
          changed = true;
        }
        if (key === 'excludedDomains' && change.newValue !== undefined) {
          this.settings.excludedDomains = change.newValue as string[];
          changed = true;
        }
        if (key === 'downloadDirectory' && change.newValue !== undefined) {
          this.settings.downloadDirectory = change.newValue as string;
          changed = true;
        }
        if (key === 'maxConnections' && change.newValue !== undefined) {
          this.settings.maxConnections = change.newValue as number;
          changed = true;
        }
        if (key === 'maxSegments' && change.newValue !== undefined) {
          this.settings.maxSegments = change.newValue as number;
          changed = true;
        }
      }
      if (changed) {
        this.notifyListeners();
      }
    }
  };

  /**
   * Notifies all listeners of state change.
   */
  private notifyListeners(): void {
    if (!this.settings || !this.sessionState) return;

    const state: ExtensionState = {
      enabled: this.settings.enabled,
      connected: this.sessionState.connected,
      activeDownloadCount: this.sessionState.activeDownloads.length,
      settings: { ...this.settings },
    };

    this.listeners.forEach((listener) => {
      try {
        listener(state);
      } catch (err) {
        console.error('State listener error:', err);
      }
    });
  }

  /**
   * Broadcasts state change to popup/options via messaging.
   */
  private broadcastStateChange(payload: StateChangePayload): void {
    chrome.runtime.sendMessage({
      type: MESSAGE_TYPES.STATE_CHANGED,
      payload,
    }).catch(() => {
      // Ignore errors when no listeners (popup closed)
    });
  }

  /**
   * Resets for testing.
   */
  reset(): void {
    // Clean up listeners (with try-catch for test environments)
    try {
      if (this.connectionListener) {
        getClient().removeStateListener(this.connectionListener);
      }
    } catch {
      // Ignore errors in test cleanup
    }
    try {
      chrome.storage.onChanged.removeListener(this.handleStorageChange);
    } catch {
      // Ignore errors in test cleanup
    }
    this.settings = null;
    this.sessionState = null;
    this.listeners.clear();
    this.connectionListener = null;
    this.initialized = false;
  }
}

/**
 * Singleton state manager instance.
 */
let stateManagerInstance: StateManager | null = null;

/**
 * Gets the state manager instance.
 */
export function getStateManager(): StateManager {
  if (!stateManagerInstance) {
    stateManagerInstance = new StateManager();
  }
  return stateManagerInstance;
}

/**
 * Resets the state manager (for testing).
 */
export function resetStateManager(): void {
  if (stateManagerInstance) {
    stateManagerInstance.reset();
  }
  stateManagerInstance = null;
}
