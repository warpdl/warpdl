/**
 * Context Menu handler for "Download with WarpDL" option.
 * Registers context menus for links, images, video, and audio elements.
 */

import { CONTEXT_MENU_IDS } from '@shared/constants';
import { downloadWithWarpDL } from './download-interceptor';
import { getClient } from './native-messaging';

/**
 * Context menu click handler.
 */
type OnClickHandler = (
  info: chrome.contextMenus.OnClickData,
  tab?: chrome.tabs.Tab
) => void;

/**
 * State for context menu management.
 */
interface ContextMenuState {
  initialized: boolean;
  clickHandler: OnClickHandler | null;
}

const state: ContextMenuState = {
  initialized: false,
  clickHandler: null,
};

/**
 * Extracts the URL to download from context menu click info.
 */
function getDownloadUrl(info: chrome.contextMenus.OnClickData): string | null {
  // Order of preference: linkUrl > srcUrl > pageUrl
  return info.linkUrl || info.srcUrl || null;
}

/**
 * Extracts a filename hint from the URL.
 */
function getFilenameHint(url: string): string | undefined {
  try {
    const urlObj = new URL(url);
    const pathParts = urlObj.pathname.split('/');
    const lastPart = pathParts[pathParts.length - 1];
    if (lastPart && lastPart.length > 0 && lastPart !== '/') {
      return decodeURIComponent(lastPart);
    }
  } catch {
    // Ignore URL parsing errors
  }
  return undefined;
}

/**
 * Handles context menu click events.
 */
async function handleContextMenuClick(
  info: chrome.contextMenus.OnClickData,
  tab?: chrome.tabs.Tab
): Promise<void> {
  const url = getDownloadUrl(info);

  if (!url) {
    console.warn('[WarpDL] No URL found in context menu click');
    return;
  }

  // Check if connected
  const client = getClient();
  if (!client.isConnected()) {
    // Show notification about not being connected
    try {
      await chrome.notifications.create({
        type: 'basic',
        iconUrl: 'assets/icons/icon48.png',
        title: 'WarpDL',
        message: 'Not connected to WarpDL daemon. Please ensure the daemon is running.',
      });
    } catch {
      console.error('[WarpDL] Failed to show notification');
    }
    return;
  }

  try {
    const filename = getFilenameHint(url);
    const referrer = tab?.url;

    const downloadId = await downloadWithWarpDL(url, filename, referrer);

    console.log(`[WarpDL] Context menu download started: ${filename || url} (${downloadId})`);

    // Show success notification
    try {
      await chrome.notifications.create({
        type: 'basic',
        iconUrl: 'assets/icons/icon48.png',
        title: 'Download Started',
        message: `WarpDL is downloading: ${filename || 'file'}`,
      });
    } catch {
      // Ignore notification errors
    }
  } catch (err) {
    console.error('[WarpDL] Context menu download failed:', err);

    // Show error notification
    try {
      await chrome.notifications.create({
        type: 'basic',
        iconUrl: 'assets/icons/icon48.png',
        title: 'Download Failed',
        message: err instanceof Error ? err.message : 'Unknown error',
      });
    } catch {
      // Ignore notification errors
    }
  }
}

/**
 * Creates the context menu items.
 */
function createMenuItems(): void {
  // Menu for links
  chrome.contextMenus.create({
    id: CONTEXT_MENU_IDS.DOWNLOAD_LINK,
    title: 'Download with WarpDL',
    contexts: ['link'],
  });

  // Menu for images
  chrome.contextMenus.create({
    id: CONTEXT_MENU_IDS.DOWNLOAD_IMAGE,
    title: 'Download image with WarpDL',
    contexts: ['image'],
  });

  // Menu for video
  chrome.contextMenus.create({
    id: CONTEXT_MENU_IDS.DOWNLOAD_VIDEO,
    title: 'Download video with WarpDL',
    contexts: ['video'],
  });

  // Menu for audio
  chrome.contextMenus.create({
    id: CONTEXT_MENU_IDS.DOWNLOAD_AUDIO,
    title: 'Download audio with WarpDL',
    contexts: ['audio'],
  });
}

/**
 * The click handler wrapper that filters by menu ID.
 */
const clickHandler: OnClickHandler = (info, tab) => {
  const validIds: string[] = [
    CONTEXT_MENU_IDS.DOWNLOAD_LINK,
    CONTEXT_MENU_IDS.DOWNLOAD_IMAGE,
    CONTEXT_MENU_IDS.DOWNLOAD_VIDEO,
    CONTEXT_MENU_IDS.DOWNLOAD_AUDIO,
  ];

  if (validIds.includes(info.menuItemId as string)) {
    handleContextMenuClick(info, tab);
  }
};

/**
 * Initializes the context menus.
 */
export async function initContextMenus(): Promise<void> {
  if (state.initialized) {
    return;
  }

  // Remove any existing menus first
  await chrome.contextMenus.removeAll();

  // Create menu items
  createMenuItems();

  // Register click handler
  state.clickHandler = clickHandler;
  chrome.contextMenus.onClicked.addListener(clickHandler);

  state.initialized = true;
  console.log('[WarpDL] Context menus initialized');
}

/**
 * Removes the context menus.
 */
export async function removeContextMenus(): Promise<void> {
  if (!state.initialized) {
    return;
  }

  // Remove click handler
  if (state.clickHandler) {
    chrome.contextMenus.onClicked.removeListener(state.clickHandler);
    state.clickHandler = null;
  }

  // Remove all menu items
  await chrome.contextMenus.removeAll();

  state.initialized = false;
  console.log('[WarpDL] Context menus removed');
}

/**
 * Checks if context menus are initialized.
 */
export function areContextMenusInitialized(): boolean {
  return state.initialized;
}
