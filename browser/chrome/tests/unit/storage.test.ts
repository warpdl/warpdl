/**
 * Storage unit tests.
 * Tests storage utilities for settings and session state.
 */

import { describe, it, expect, beforeEach } from 'vitest';
import {
  resetMockChrome,
  setMockSyncStorage,
  setMockSessionStorage,
  getMockSyncStorage,
  getMockSessionStorage,
} from '../mocks/chrome-api';
import {
  loadSettings,
  saveSettings,
  loadSessionState,
  saveSessionState,
  addActiveDownload,
  removeActiveDownload,
  isDomainExcluded,
  isAboveThreshold,
  resetSettings,
  clearSessionState,
  DEFAULT_SETTINGS,
  DEFAULT_SESSION_STATE,
} from '@shared/storage';
import { STORAGE_KEYS, SESSION_KEYS } from '@shared/constants';

describe('storage utilities', () => {
  beforeEach(() => {
    resetMockChrome();
  });

  describe('loadSettings', () => {
    it('returns default settings when storage is empty', async () => {
      const settings = await loadSettings();
      expect(settings).toEqual(DEFAULT_SETTINGS);
    });

    it('returns stored settings', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: false,
        [STORAGE_KEYS.SIZE_THRESHOLD]: 2048,
        [STORAGE_KEYS.EXCLUDED_DOMAINS]: ['example.com'],
        [STORAGE_KEYS.DOWNLOAD_DIRECTORY]: '/custom/downloads',
        [STORAGE_KEYS.MAX_CONNECTIONS]: 16,
        [STORAGE_KEYS.MAX_SEGMENTS]: 32,
      });

      const settings = await loadSettings();
      expect(settings).toEqual({
        enabled: false,
        sizeThreshold: 2048,
        excludedDomains: ['example.com'],
        downloadDirectory: '/custom/downloads',
        maxConnections: 16,
        maxSegments: 32,
      });
    });

    it('uses defaults for missing values', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: false,
      });

      const settings = await loadSettings();
      expect(settings.enabled).toBe(false);
      expect(settings.sizeThreshold).toBe(DEFAULT_SETTINGS.sizeThreshold);
      expect(settings.excludedDomains).toEqual(DEFAULT_SETTINGS.excludedDomains);
    });
  });

  describe('saveSettings', () => {
    it('saves partial settings', async () => {
      await saveSettings({ enabled: false });
      const storage = getMockSyncStorage();
      expect(storage[STORAGE_KEYS.ENABLED]).toBe(false);
      expect(storage[STORAGE_KEYS.SIZE_THRESHOLD]).toBeUndefined();
    });

    it('saves all settings', async () => {
      await saveSettings({
        enabled: true,
        sizeThreshold: 5000,
        excludedDomains: ['test.com'],
        downloadDirectory: '/test',
        maxConnections: 4,
        maxSegments: 8,
      });

      const storage = getMockSyncStorage();
      expect(storage[STORAGE_KEYS.ENABLED]).toBe(true);
      expect(storage[STORAGE_KEYS.SIZE_THRESHOLD]).toBe(5000);
      expect(storage[STORAGE_KEYS.EXCLUDED_DOMAINS]).toEqual(['test.com']);
      expect(storage[STORAGE_KEYS.DOWNLOAD_DIRECTORY]).toBe('/test');
      expect(storage[STORAGE_KEYS.MAX_CONNECTIONS]).toBe(4);
      expect(storage[STORAGE_KEYS.MAX_SEGMENTS]).toBe(8);
    });

    it('does nothing when no settings provided', async () => {
      await saveSettings({});
      expect(getMockSyncStorage()).toEqual({});
    });
  });

  describe('loadSessionState', () => {
    it('returns default state when storage is empty', async () => {
      const state = await loadSessionState();
      expect(state).toEqual(DEFAULT_SESSION_STATE);
    });

    it('returns stored state', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.CONNECTED]: true,
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1', 'dl-2'],
      });

      const state = await loadSessionState();
      expect(state).toEqual({
        connected: true,
        activeDownloads: ['dl-1', 'dl-2'],
      });
    });
  });

  describe('saveSessionState', () => {
    it('saves connected state', async () => {
      await saveSessionState({ connected: true });
      const storage = getMockSessionStorage();
      expect(storage[SESSION_KEYS.CONNECTED]).toBe(true);
    });

    it('saves active downloads', async () => {
      await saveSessionState({ activeDownloads: ['dl-1'] });
      const storage = getMockSessionStorage();
      expect(storage[SESSION_KEYS.ACTIVE_DOWNLOADS]).toEqual(['dl-1']);
    });
  });

  describe('addActiveDownload', () => {
    it('adds download to empty list', async () => {
      await addActiveDownload('dl-1');
      const state = await loadSessionState();
      expect(state.activeDownloads).toEqual(['dl-1']);
    });

    it('adds download to existing list', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1'],
      });

      await addActiveDownload('dl-2');
      const state = await loadSessionState();
      expect(state.activeDownloads).toEqual(['dl-1', 'dl-2']);
    });

    it('does not add duplicate', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1'],
      });

      await addActiveDownload('dl-1');
      const state = await loadSessionState();
      expect(state.activeDownloads).toEqual(['dl-1']);
    });
  });

  describe('removeActiveDownload', () => {
    it('removes download from list', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1', 'dl-2'],
      });

      await removeActiveDownload('dl-1');
      const state = await loadSessionState();
      expect(state.activeDownloads).toEqual(['dl-2']);
    });

    it('handles removal of non-existent download', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1'],
      });

      await removeActiveDownload('dl-2');
      const state = await loadSessionState();
      expect(state.activeDownloads).toEqual(['dl-1']);
    });
  });

  describe('isDomainExcluded', () => {
    it('returns false when no domains excluded', async () => {
      expect(await isDomainExcluded('example.com')).toBe(false);
    });

    it('returns true for exact match', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.EXCLUDED_DOMAINS]: ['example.com'],
      });

      expect(await isDomainExcluded('example.com')).toBe(true);
    });

    it('returns true for subdomain match', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.EXCLUDED_DOMAINS]: ['example.com'],
      });

      expect(await isDomainExcluded('sub.example.com')).toBe(true);
      expect(await isDomainExcluded('deep.sub.example.com')).toBe(true);
    });

    it('is case-insensitive', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.EXCLUDED_DOMAINS]: ['EXAMPLE.COM'],
      });

      expect(await isDomainExcluded('example.com')).toBe(true);
      expect(await isDomainExcluded('Example.Com')).toBe(true);
    });

    it('returns false for partial matches', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.EXCLUDED_DOMAINS]: ['example.com'],
      });

      expect(await isDomainExcluded('notexample.com')).toBe(false);
      expect(await isDomainExcluded('example.com.other')).toBe(false);
    });
  });

  describe('isAboveThreshold', () => {
    it('returns true for size at threshold', async () => {
      expect(await isAboveThreshold(DEFAULT_SETTINGS.sizeThreshold)).toBe(true);
    });

    it('returns true for size above threshold', async () => {
      expect(await isAboveThreshold(DEFAULT_SETTINGS.sizeThreshold + 1)).toBe(true);
    });

    it('returns false for size below threshold', async () => {
      expect(await isAboveThreshold(DEFAULT_SETTINGS.sizeThreshold - 1)).toBe(false);
    });

    it('uses custom threshold', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.SIZE_THRESHOLD]: 5000,
      });

      expect(await isAboveThreshold(5000)).toBe(true);
      expect(await isAboveThreshold(4999)).toBe(false);
    });
  });

  describe('resetSettings', () => {
    it('resets all settings to defaults', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: false,
        [STORAGE_KEYS.SIZE_THRESHOLD]: 9999,
      });

      await resetSettings();
      const settings = await loadSettings();
      expect(settings).toEqual(DEFAULT_SETTINGS);
    });
  });

  describe('clearSessionState', () => {
    it('clears all session state', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.CONNECTED]: true,
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1'],
      });

      await clearSessionState();
      const state = await loadSessionState();
      expect(state).toEqual(DEFAULT_SESSION_STATE);
    });
  });
});
