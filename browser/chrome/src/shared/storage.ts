/**
 * Storage utilities for chrome.storage.sync and chrome.storage.session.
 * Provides type-safe access to extension settings and session state.
 */

import {
  STORAGE_KEYS,
  SESSION_KEYS,
  DEFAULT_SIZE_THRESHOLD_BYTES,
  DEFAULT_MAX_CONNECTIONS,
  DEFAULT_MAX_SEGMENTS,
} from './constants';
import type { ExtensionSettings, ExtensionSessionState } from './types';

/**
 * Default extension settings.
 */
export const DEFAULT_SETTINGS: ExtensionSettings = {
  enabled: true,
  sizeThreshold: DEFAULT_SIZE_THRESHOLD_BYTES,
  excludedDomains: [],
  downloadDirectory: '',
  maxConnections: DEFAULT_MAX_CONNECTIONS,
  maxSegments: DEFAULT_MAX_SEGMENTS,
};

/**
 * Default session state.
 */
export const DEFAULT_SESSION_STATE: ExtensionSessionState = {
  connected: false,
  activeDownloads: [],
};

/**
 * Loads extension settings from chrome.storage.sync.
 * Returns defaults for any missing values.
 */
export async function loadSettings(): Promise<ExtensionSettings> {
  const result = await chrome.storage.sync.get([
    STORAGE_KEYS.ENABLED,
    STORAGE_KEYS.SIZE_THRESHOLD,
    STORAGE_KEYS.EXCLUDED_DOMAINS,
    STORAGE_KEYS.DOWNLOAD_DIRECTORY,
    STORAGE_KEYS.MAX_CONNECTIONS,
    STORAGE_KEYS.MAX_SEGMENTS,
  ]);

  return {
    enabled: result[STORAGE_KEYS.ENABLED] ?? DEFAULT_SETTINGS.enabled,
    sizeThreshold: result[STORAGE_KEYS.SIZE_THRESHOLD] ?? DEFAULT_SETTINGS.sizeThreshold,
    excludedDomains: result[STORAGE_KEYS.EXCLUDED_DOMAINS] ?? DEFAULT_SETTINGS.excludedDomains,
    downloadDirectory: result[STORAGE_KEYS.DOWNLOAD_DIRECTORY] ?? DEFAULT_SETTINGS.downloadDirectory,
    maxConnections: result[STORAGE_KEYS.MAX_CONNECTIONS] ?? DEFAULT_SETTINGS.maxConnections,
    maxSegments: result[STORAGE_KEYS.MAX_SEGMENTS] ?? DEFAULT_SETTINGS.maxSegments,
  };
}

/**
 * Saves extension settings to chrome.storage.sync.
 * Only saves the provided fields, preserving others.
 */
export async function saveSettings(settings: Partial<ExtensionSettings>): Promise<void> {
  const data: Record<string, unknown> = {};

  if (settings.enabled !== undefined) {
    data[STORAGE_KEYS.ENABLED] = settings.enabled;
  }
  if (settings.sizeThreshold !== undefined) {
    data[STORAGE_KEYS.SIZE_THRESHOLD] = settings.sizeThreshold;
  }
  if (settings.excludedDomains !== undefined) {
    data[STORAGE_KEYS.EXCLUDED_DOMAINS] = settings.excludedDomains;
  }
  if (settings.downloadDirectory !== undefined) {
    data[STORAGE_KEYS.DOWNLOAD_DIRECTORY] = settings.downloadDirectory;
  }
  if (settings.maxConnections !== undefined) {
    data[STORAGE_KEYS.MAX_CONNECTIONS] = settings.maxConnections;
  }
  if (settings.maxSegments !== undefined) {
    data[STORAGE_KEYS.MAX_SEGMENTS] = settings.maxSegments;
  }

  if (Object.keys(data).length > 0) {
    await chrome.storage.sync.set(data);
  }
}

/**
 * Loads session state from chrome.storage.session.
 * Returns defaults for any missing values.
 */
export async function loadSessionState(): Promise<ExtensionSessionState> {
  const result = await chrome.storage.session.get([
    SESSION_KEYS.CONNECTED,
    SESSION_KEYS.ACTIVE_DOWNLOADS,
  ]);

  return {
    connected: result[SESSION_KEYS.CONNECTED] ?? DEFAULT_SESSION_STATE.connected,
    activeDownloads: result[SESSION_KEYS.ACTIVE_DOWNLOADS] ?? DEFAULT_SESSION_STATE.activeDownloads,
  };
}

/**
 * Saves session state to chrome.storage.session.
 * Only saves the provided fields, preserving others.
 */
export async function saveSessionState(state: Partial<ExtensionSessionState>): Promise<void> {
  const data: Record<string, unknown> = {};

  if (state.connected !== undefined) {
    data[SESSION_KEYS.CONNECTED] = state.connected;
  }
  if (state.activeDownloads !== undefined) {
    data[SESSION_KEYS.ACTIVE_DOWNLOADS] = state.activeDownloads;
  }

  if (Object.keys(data).length > 0) {
    await chrome.storage.session.set(data);
  }
}

/**
 * Adds a download ID to the active downloads list.
 */
export async function addActiveDownload(downloadId: string): Promise<void> {
  const state = await loadSessionState();
  if (!state.activeDownloads.includes(downloadId)) {
    state.activeDownloads.push(downloadId);
    await saveSessionState({ activeDownloads: state.activeDownloads });
  }
}

/**
 * Removes a download ID from the active downloads list.
 */
export async function removeActiveDownload(downloadId: string): Promise<void> {
  const state = await loadSessionState();
  const index = state.activeDownloads.indexOf(downloadId);
  if (index !== -1) {
    state.activeDownloads.splice(index, 1);
    await saveSessionState({ activeDownloads: state.activeDownloads });
  }
}

/**
 * Checks if a domain is excluded from download interception.
 */
export async function isDomainExcluded(domain: string): Promise<boolean> {
  const settings = await loadSettings();
  const normalizedDomain = domain.toLowerCase();
  return settings.excludedDomains.some(
    (excluded) => normalizedDomain === excluded.toLowerCase() || normalizedDomain.endsWith('.' + excluded.toLowerCase())
  );
}

/**
 * Checks if a file size is above the interception threshold.
 */
export async function isAboveThreshold(size: number): Promise<boolean> {
  const settings = await loadSettings();
  return size >= settings.sizeThreshold;
}

/**
 * Resets all settings to defaults.
 */
export async function resetSettings(): Promise<void> {
  await chrome.storage.sync.clear();
  await saveSettings(DEFAULT_SETTINGS);
}

/**
 * Clears all session state.
 */
export async function clearSessionState(): Promise<void> {
  await chrome.storage.session.clear();
}
