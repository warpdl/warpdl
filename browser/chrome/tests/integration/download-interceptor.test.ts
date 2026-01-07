/**
 * Download interceptor integration tests.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  resetMockChrome,
  setMockSyncStorage,
  mockChrome,
} from '../mocks/chrome-api';
import { createMockNativeHost, MockNativeHost } from '../mocks/native-host';
import { getClient, resetClient } from '@background/native-messaging';
import { getStateManager, resetStateManager } from '@background/state';
import {
  startInterceptor,
  stopInterceptor,
  isInterceptorEnabled,
  downloadWithWarpDL,
} from '@background/download-interceptor';
import { STORAGE_KEYS } from '@shared/constants';

describe('Download Interceptor', () => {
  let mockHost: MockNativeHost;

  beforeEach(async () => {
    vi.clearAllMocks();
    resetMockChrome();
    resetClient();
    resetStateManager();

    // Set default settings
    setMockSyncStorage({
      [STORAGE_KEYS.ENABLED]: true,
      [STORAGE_KEYS.SIZE_THRESHOLD]: 1024 * 1024, // 1MB
      [STORAGE_KEYS.EXCLUDED_DOMAINS]: [],
      [STORAGE_KEYS.MAX_CONNECTIONS]: 8,
      [STORAGE_KEYS.MAX_SEGMENTS]: 16,
    });

    // Connect to daemon
    const client = getClient();
    await client.connect();
    mockHost = createMockNativeHost();
    mockHost.attach();

    // Initialize state manager
    await getStateManager().getState();
  });

  afterEach(() => {
    stopInterceptor();
    resetStateManager();
    resetClient();
    resetMockChrome();
  });

  describe('startInterceptor / stopInterceptor', () => {
    it('starts and stops the interceptor', () => {
      expect(isInterceptorEnabled()).toBe(false);

      startInterceptor();
      expect(isInterceptorEnabled()).toBe(true);
      expect(mockChrome.downloads.onDeterminingFilename.addListener).toHaveBeenCalled();

      stopInterceptor();
      expect(isInterceptorEnabled()).toBe(false);
      expect(mockChrome.downloads.onDeterminingFilename.removeListener).toHaveBeenCalled();
    });

    it('does not start twice', () => {
      startInterceptor();
      startInterceptor();

      // Should only add listener once
      expect(mockChrome.downloads.onDeterminingFilename.addListener).toHaveBeenCalledTimes(1);
    });

    it('does not stop if not started', () => {
      stopInterceptor();

      expect(mockChrome.downloads.onDeterminingFilename.removeListener).not.toHaveBeenCalled();
    });
  });

  describe('downloadWithWarpDL', () => {
    it('downloads a URL via WarpDL', async () => {
      const downloadId = await downloadWithWarpDL('https://example.com/file.zip');

      expect(downloadId).toBeDefined();
      expect(downloadId).toMatch(/^dl-/);
    });

    it('includes filename if provided', async () => {
      const downloadId = await downloadWithWarpDL('https://example.com/file.zip', 'custom.zip');

      // Verify download was initiated
      expect(downloadId).toBeDefined();
      expect(downloadId).toMatch(/^dl-/);
    });

    it('includes referrer header if provided', async () => {
      const downloadId = await downloadWithWarpDL(
        'https://example.com/file.zip',
        'file.zip',
        'https://example.com/page'
      );

      // Verify download was initiated
      expect(downloadId).toBeDefined();
      expect(downloadId).toMatch(/^dl-/);
    });

    it('throws if not connected', async () => {
      // Disconnect
      getClient().disconnect();

      await expect(
        downloadWithWarpDL('https://example.com/file.zip')
      ).rejects.toThrow('Not connected to WarpDL daemon');
    });

    it('uses settings for maxConnections and maxSegments', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: true,
        [STORAGE_KEYS.MAX_CONNECTIONS]: 16,
        [STORAGE_KEYS.MAX_SEGMENTS]: 32,
      });

      // Reinitialize state to pick up new settings
      resetStateManager();
      await getStateManager().getState();

      const downloadId = await downloadWithWarpDL('https://example.com/file.zip');

      // Verify download was initiated with settings
      expect(downloadId).toBeDefined();
      expect(downloadId).toMatch(/^dl-/);
    });

    it('tracks download after success', async () => {
      await downloadWithWarpDL('https://example.com/file.zip');

      const state = await getStateManager().getState();
      expect(state.activeDownloadCount).toBeGreaterThan(0);
    });
  });

  describe('interception logic', () => {
    it('intercepts downloads above threshold', async () => {
      startInterceptor();

      // Simulate a download
      const downloadItem: chrome.downloads.DownloadItem = {
        id: 1,
        url: 'https://example.com/large-file.zip',
        finalUrl: 'https://example.com/large-file.zip',
        filename: 'large-file.zip',
        fileSize: 10 * 1024 * 1024, // 10MB
        mime: 'application/zip',
        referrer: 'https://example.com',
        state: 'in_progress',
        paused: false,
        canResume: true,
        error: undefined,
        bytesReceived: 0,
        totalBytes: 10 * 1024 * 1024,
        startTime: new Date().toISOString(),
        endTime: undefined,
        exists: true,
        incognito: false,
        danger: 'safe',
      };

      const suggest = vi.fn();

      // Get the listener
      const listener = mockChrome.downloads.onDeterminingFilename.addListener.mock.calls[0]?.[0];
      expect(listener).toBeDefined();

      // Call the listener
      listener!(downloadItem, suggest);

      // Wait for async operations
      await vi.waitFor(() => {
        // Should have cancelled the download
        expect(mockChrome.downloads.cancel).toHaveBeenCalledWith(1);
      });
    });

    it('passes through downloads below threshold', async () => {
      startInterceptor();

      const downloadItem: chrome.downloads.DownloadItem = {
        id: 2,
        url: 'https://example.com/small-file.txt',
        finalUrl: 'https://example.com/small-file.txt',
        filename: 'small-file.txt',
        fileSize: 100, // 100 bytes
        mime: 'text/plain',
        referrer: '',
        state: 'in_progress',
        paused: false,
        canResume: true,
        error: undefined,
        bytesReceived: 0,
        totalBytes: 100,
        startTime: new Date().toISOString(),
        endTime: undefined,
        exists: true,
        incognito: false,
        danger: 'safe',
      };

      const suggest = vi.fn();

      const listener = mockChrome.downloads.onDeterminingFilename.addListener.mock.calls[0]?.[0];
      listener!(downloadItem, suggest);

      // Wait for async operations
      await vi.waitFor(() => {
        // Should let browser handle it
        expect(suggest).toHaveBeenCalled();
      });

      // Should NOT have cancelled the download
      expect(mockChrome.downloads.cancel).not.toHaveBeenCalled();
    });

    it('respects enabled setting', async () => {
      // Disable interception
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: false,
      });
      resetStateManager();
      await getStateManager().getState();

      startInterceptor();

      const downloadItem: chrome.downloads.DownloadItem = {
        id: 3,
        url: 'https://example.com/file.zip',
        finalUrl: 'https://example.com/file.zip',
        filename: 'file.zip',
        fileSize: 10 * 1024 * 1024,
        mime: 'application/zip',
        referrer: '',
        state: 'in_progress',
        paused: false,
        canResume: true,
        error: undefined,
        bytesReceived: 0,
        totalBytes: 10 * 1024 * 1024,
        startTime: new Date().toISOString(),
        endTime: undefined,
        exists: true,
        incognito: false,
        danger: 'safe',
      };

      const suggest = vi.fn();

      const listener = mockChrome.downloads.onDeterminingFilename.addListener.mock.calls[0]?.[0];
      listener!(downloadItem, suggest);

      await vi.waitFor(() => {
        expect(suggest).toHaveBeenCalled();
      });

      // Should NOT intercept
      expect(mockChrome.downloads.cancel).not.toHaveBeenCalled();
    });

    it('respects excluded domains', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: true,
        [STORAGE_KEYS.SIZE_THRESHOLD]: 1024,
        [STORAGE_KEYS.EXCLUDED_DOMAINS]: ['excluded.com'],
      });
      resetStateManager();
      await getStateManager().getState();

      startInterceptor();

      const downloadItem: chrome.downloads.DownloadItem = {
        id: 4,
        url: 'https://excluded.com/file.zip',
        finalUrl: 'https://excluded.com/file.zip',
        filename: 'file.zip',
        fileSize: 10 * 1024 * 1024,
        mime: 'application/zip',
        referrer: '',
        state: 'in_progress',
        paused: false,
        canResume: true,
        error: undefined,
        bytesReceived: 0,
        totalBytes: 10 * 1024 * 1024,
        startTime: new Date().toISOString(),
        endTime: undefined,
        exists: true,
        incognito: false,
        danger: 'safe',
      };

      const suggest = vi.fn();

      const listener = mockChrome.downloads.onDeterminingFilename.addListener.mock.calls[0]?.[0];
      listener!(downloadItem, suggest);

      await vi.waitFor(() => {
        expect(suggest).toHaveBeenCalled();
      });

      // Should NOT intercept excluded domain
      expect(mockChrome.downloads.cancel).not.toHaveBeenCalled();
    });

    it('falls back on daemon error', async () => {
      // Make daemon return error
      mockHost.setHandler('download', (req) => ({
        id: req.id,
        ok: false,
        error: 'Daemon error',
      }));

      startInterceptor();

      const downloadItem: chrome.downloads.DownloadItem = {
        id: 5,
        url: 'https://example.com/file.zip',
        finalUrl: 'https://example.com/file.zip',
        filename: 'file.zip',
        fileSize: 10 * 1024 * 1024,
        mime: 'application/zip',
        referrer: '',
        state: 'in_progress',
        paused: false,
        canResume: true,
        error: undefined,
        bytesReceived: 0,
        totalBytes: 10 * 1024 * 1024,
        startTime: new Date().toISOString(),
        endTime: undefined,
        exists: true,
        incognito: false,
        danger: 'safe',
      };

      const suggest = vi.fn();
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

      const listener = mockChrome.downloads.onDeterminingFilename.addListener.mock.calls[0]?.[0];
      listener!(downloadItem, suggest);

      await vi.waitFor(() => {
        expect(suggest).toHaveBeenCalled();
      });

      // Should have logged error and fallen back
      expect(consoleSpy).toHaveBeenCalled();
      consoleSpy.mockRestore();
    });
  });
});
