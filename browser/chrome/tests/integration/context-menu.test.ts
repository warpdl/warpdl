/**
 * Context menu integration tests.
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
  initContextMenus,
  removeContextMenus,
  areContextMenusInitialized,
} from '@background/context-menu';
import { CONTEXT_MENU_IDS, STORAGE_KEYS } from '@shared/constants';

describe('Context Menu', () => {
  let mockHost: MockNativeHost;

  beforeEach(async () => {
    vi.clearAllMocks();
    resetMockChrome();
    resetClient();
    resetStateManager();

    // Set default settings
    setMockSyncStorage({
      [STORAGE_KEYS.ENABLED]: true,
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

  afterEach(async () => {
    await removeContextMenus();
    resetStateManager();
    resetClient();
    resetMockChrome();
  });

  describe('initContextMenus / removeContextMenus', () => {
    it('initializes context menus', async () => {
      expect(areContextMenusInitialized()).toBe(false);

      await initContextMenus();

      expect(areContextMenusInitialized()).toBe(true);
      expect(mockChrome.contextMenus.create).toHaveBeenCalledTimes(4);
      expect(mockChrome.contextMenus.onClicked.addListener).toHaveBeenCalled();
    });

    it('creates menu for links', async () => {
      await initContextMenus();

      expect(mockChrome.contextMenus.create).toHaveBeenCalledWith(
        expect.objectContaining({
          id: CONTEXT_MENU_IDS.DOWNLOAD_LINK,
          contexts: ['link'],
        })
      );
    });

    it('creates menu for images', async () => {
      await initContextMenus();

      expect(mockChrome.contextMenus.create).toHaveBeenCalledWith(
        expect.objectContaining({
          id: CONTEXT_MENU_IDS.DOWNLOAD_IMAGE,
          contexts: ['image'],
        })
      );
    });

    it('creates menu for video', async () => {
      await initContextMenus();

      expect(mockChrome.contextMenus.create).toHaveBeenCalledWith(
        expect.objectContaining({
          id: CONTEXT_MENU_IDS.DOWNLOAD_VIDEO,
          contexts: ['video'],
        })
      );
    });

    it('creates menu for audio', async () => {
      await initContextMenus();

      expect(mockChrome.contextMenus.create).toHaveBeenCalledWith(
        expect.objectContaining({
          id: CONTEXT_MENU_IDS.DOWNLOAD_AUDIO,
          contexts: ['audio'],
        })
      );
    });

    it('does not initialize twice', async () => {
      await initContextMenus();
      await initContextMenus();

      expect(mockChrome.contextMenus.create).toHaveBeenCalledTimes(4);
    });

    it('removes context menus', async () => {
      await initContextMenus();
      await removeContextMenus();

      expect(areContextMenusInitialized()).toBe(false);
      expect(mockChrome.contextMenus.removeAll).toHaveBeenCalled();
      expect(mockChrome.contextMenus.onClicked.removeListener).toHaveBeenCalled();
    });

    it('does not remove if not initialized', async () => {
      await removeContextMenus();

      expect(mockChrome.contextMenus.removeAll).not.toHaveBeenCalled();
    });
  });

  describe('click handling', () => {
    beforeEach(async () => {
      await initContextMenus();
    });

    it('downloads link on click', async () => {
      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];
      expect(clickHandler).toBeDefined();

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_LINK,
        linkUrl: 'https://example.com/file.zip',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      const tab: chrome.tabs.Tab = {
        id: 1,
        index: 0,
        highlighted: true,
        active: true,
        pinned: false,
        incognito: false,
        url: 'https://example.com/',
        windowId: 1,
        selected: true,
        discarded: false,
        autoDiscardable: true,
        groupId: -1,
      };

      clickHandler!(info, tab);

      // Wait for async operations
      await vi.waitFor(() => {
        // Should have shown success notification
        expect(mockChrome.notifications.create).toHaveBeenCalled();
      });
    });

    it('downloads image on click', async () => {
      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_IMAGE,
        srcUrl: 'https://example.com/image.png',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      await vi.waitFor(() => {
        expect(mockChrome.notifications.create).toHaveBeenCalled();
      });
    });

    it('downloads video on click', async () => {
      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_VIDEO,
        srcUrl: 'https://example.com/video.mp4',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      await vi.waitFor(() => {
        expect(mockChrome.notifications.create).toHaveBeenCalled();
      });
    });

    it('downloads audio on click', async () => {
      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_AUDIO,
        srcUrl: 'https://example.com/audio.mp3',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      await vi.waitFor(() => {
        expect(mockChrome.notifications.create).toHaveBeenCalled();
      });
    });

    it('ignores clicks on unknown menu items', async () => {
      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: 'unknown-menu-id',
        linkUrl: 'https://example.com/file.zip',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      // Wait a bit
      await new Promise((r) => setTimeout(r, 50));

      // Should not have made any download
      expect(mockChrome.notifications.create).not.toHaveBeenCalled();
    });

    it('shows notification when daemon not connected', async () => {
      // Disconnect from daemon
      getClient().disconnect();

      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_LINK,
        linkUrl: 'https://example.com/file.zip',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      await vi.waitFor(() => {
        expect(mockChrome.notifications.create).toHaveBeenCalledWith(
          expect.objectContaining({
            message: expect.stringContaining('Not connected'),
          })
        );
      });
    });

    it('shows error notification on failure', async () => {
      // Make daemon return error
      mockHost.setHandler('download', (req) => ({
        id: req.id,
        ok: false,
        error: 'Download failed',
      }));

      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_LINK,
        linkUrl: 'https://example.com/file.zip',
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      await vi.waitFor(() => {
        expect(mockChrome.notifications.create).toHaveBeenCalledWith(
          expect.objectContaining({
            title: 'Download Failed',
          })
        );
      });
    });

    it('warns when no URL found', async () => {
      const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

      const clickHandler = mockChrome.contextMenus.onClicked.addListener.mock.calls[0]?.[0];

      const info: chrome.contextMenus.OnClickData = {
        menuItemId: CONTEXT_MENU_IDS.DOWNLOAD_LINK,
        // No URL provided
        editable: false,
        pageUrl: 'https://example.com/',
      };

      clickHandler!(info, undefined);

      await new Promise((r) => setTimeout(r, 50));

      expect(consoleSpy).toHaveBeenCalledWith(
        expect.stringContaining('No URL found')
      );

      consoleSpy.mockRestore();
    });
  });
});
