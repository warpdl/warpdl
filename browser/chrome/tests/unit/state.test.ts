/**
 * State management unit tests.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  resetMockChrome,
  setMockSyncStorage,
  setMockSessionStorage,
  getMockSyncStorage,
  getMockSessionStorage,
  mockChrome,
} from '../mocks/chrome-api';
import { getStateManager, resetStateManager, type ExtensionState } from '@background/state';
import { resetClient } from '@background/native-messaging';
import { STORAGE_KEYS, SESSION_KEYS } from '@shared/constants';
import { DEFAULT_SETTINGS } from '@shared/storage';

describe('StateManager', () => {
  beforeEach(() => {
    // Order matters: reset mocks first, then singletons that may use them
    vi.clearAllMocks();
    resetMockChrome();
    resetClient();
    resetStateManager();
  });

  afterEach(() => {
    // Order matters: reset singletons first while mocks still exist, then mocks
    resetStateManager();
    resetClient();
    resetMockChrome();
  });

  describe('initialization', () => {
    it('loads settings and session state on initialize', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: false,
        [STORAGE_KEYS.SIZE_THRESHOLD]: 5000,
      });
      setMockSessionStorage({
        [SESSION_KEYS.CONNECTED]: true,
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1'],
      });

      const manager = getStateManager();
      const state = await manager.getState();

      expect(state.enabled).toBe(false);
      expect(state.connected).toBe(true);
      expect(state.activeDownloadCount).toBe(1);
      expect(state.settings.sizeThreshold).toBe(5000);
    });

    it('uses defaults when storage is empty', async () => {
      const manager = getStateManager();
      const state = await manager.getState();

      expect(state.enabled).toBe(DEFAULT_SETTINGS.enabled);
      expect(state.connected).toBe(false);
      expect(state.activeDownloadCount).toBe(0);
    });

    it('only initializes once', async () => {
      const manager = getStateManager();
      await manager.getState();
      await manager.getState();

      // storage.sync.get should only be called once for settings
      expect(mockChrome.storage.sync.get).toHaveBeenCalledTimes(1);
    });
  });

  describe('getSettings', () => {
    it('returns null before initialization', () => {
      const manager = getStateManager();
      expect(manager.getSettings()).toBeNull();
    });

    it('returns settings after initialization', async () => {
      setMockSyncStorage({
        [STORAGE_KEYS.ENABLED]: true,
        [STORAGE_KEYS.MAX_CONNECTIONS]: 16,
      });

      const manager = getStateManager();
      await manager.getState(); // Initialize

      const settings = manager.getSettings();
      expect(settings).not.toBeNull();
      expect(settings!.enabled).toBe(true);
      expect(settings!.maxConnections).toBe(16);
    });

    it('returns a copy, not the original', async () => {
      const manager = getStateManager();
      await manager.getState();

      const settings1 = manager.getSettings();
      const settings2 = manager.getSettings();

      expect(settings1).not.toBe(settings2);
    });
  });

  describe('getSessionState', () => {
    it('returns null before initialization', () => {
      const manager = getStateManager();
      expect(manager.getSessionState()).toBeNull();
    });

    it('returns session state after initialization', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.CONNECTED]: true,
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1', 'dl-2'],
      });

      const manager = getStateManager();
      await manager.getState();

      const session = manager.getSessionState();
      expect(session).not.toBeNull();
      expect(session!.connected).toBe(true);
      expect(session!.activeDownloads).toHaveLength(2);
    });
  });

  describe('toggleEnabled', () => {
    it('toggles enabled state', async () => {
      setMockSyncStorage({ [STORAGE_KEYS.ENABLED]: true });

      const manager = getStateManager();
      await manager.getState();

      const newValue = await manager.toggleEnabled();
      expect(newValue).toBe(false);

      const state = await manager.getState();
      expect(state.enabled).toBe(false);
    });

    it('persists to storage', async () => {
      setMockSyncStorage({ [STORAGE_KEYS.ENABLED]: true });

      const manager = getStateManager();
      await manager.getState();
      await manager.toggleEnabled();

      expect(getMockSyncStorage()[STORAGE_KEYS.ENABLED]).toBe(false);
    });

    it('notifies listeners', async () => {
      const manager = getStateManager();
      await manager.getState();

      const states: ExtensionState[] = [];
      manager.addListener((state) => states.push(state));

      await manager.toggleEnabled();

      expect(states).toHaveLength(1);
      expect(states[0]!.enabled).toBe(false);
    });

    it('broadcasts state change', async () => {
      const manager = getStateManager();
      await manager.getState();
      await manager.toggleEnabled();

      expect(mockChrome.runtime.sendMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'stateChanged',
          payload: expect.objectContaining({ enabled: false }),
        })
      );
    });
  });

  describe('setEnabled', () => {
    it('sets enabled state', async () => {
      const manager = getStateManager();
      await manager.getState();

      await manager.setEnabled(false);

      const state = await manager.getState();
      expect(state.enabled).toBe(false);
    });

    it('does nothing if value unchanged', async () => {
      setMockSyncStorage({ [STORAGE_KEYS.ENABLED]: true });

      const manager = getStateManager();
      await manager.getState();

      const listener = vi.fn();
      manager.addListener(listener);

      await manager.setEnabled(true); // Same value

      expect(listener).not.toHaveBeenCalled();
    });
  });

  describe('updateSettings', () => {
    it('updates partial settings', async () => {
      const manager = getStateManager();
      await manager.getState();

      await manager.updateSettings({ maxConnections: 32 });

      const settings = manager.getSettings();
      expect(settings!.maxConnections).toBe(32);
      expect(settings!.enabled).toBe(DEFAULT_SETTINGS.enabled);
    });

    it('persists to storage', async () => {
      const manager = getStateManager();
      await manager.getState();

      await manager.updateSettings({
        sizeThreshold: 10000,
        excludedDomains: ['example.com'],
      });

      const storage = getMockSyncStorage();
      expect(storage[STORAGE_KEYS.SIZE_THRESHOLD]).toBe(10000);
      expect(storage[STORAGE_KEYS.EXCLUDED_DOMAINS]).toEqual(['example.com']);
    });
  });

  describe('download tracking', () => {
    it('trackDownload adds to list', async () => {
      const manager = getStateManager();
      await manager.getState();

      await manager.trackDownload('dl-123');

      const state = await manager.getState();
      expect(state.activeDownloadCount).toBe(1);
    });

    it('trackDownload persists to session storage', async () => {
      const manager = getStateManager();
      await manager.getState();

      await manager.trackDownload('dl-persist-test');

      const storage = getMockSessionStorage();
      expect(storage[SESSION_KEYS.ACTIVE_DOWNLOADS]).toContain('dl-persist-test');
    });

    it('untrackDownload removes from list', async () => {
      setMockSessionStorage({
        [SESSION_KEYS.ACTIVE_DOWNLOADS]: ['dl-1', 'dl-2'],
      });

      const manager = getStateManager();
      await manager.getState();

      await manager.untrackDownload('dl-1');

      const state = await manager.getState();
      expect(state.activeDownloadCount).toBe(1);
    });

    it('broadcasts count change', async () => {
      const manager = getStateManager();
      const initState = await manager.getState();
      const initialCount = initState.activeDownloadCount;

      await manager.trackDownload('dl-broadcast-test');

      expect(mockChrome.runtime.sendMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'stateChanged',
          payload: expect.objectContaining({ activeDownloadCount: initialCount + 1 }),
        })
      );
    });
  });

  describe('listeners', () => {
    it('notifies multiple listeners', async () => {
      const manager = getStateManager();
      await manager.getState();

      const listener1 = vi.fn();
      const listener2 = vi.fn();

      manager.addListener(listener1);
      manager.addListener(listener2);

      await manager.toggleEnabled();

      expect(listener1).toHaveBeenCalled();
      expect(listener2).toHaveBeenCalled();
    });

    it('removeListener stops notifications', async () => {
      const manager = getStateManager();
      await manager.getState();

      const listener = vi.fn();
      manager.addListener(listener);
      manager.removeListener(listener);

      await manager.toggleEnabled();

      expect(listener).not.toHaveBeenCalled();
    });

    it('handles listener errors gracefully', async () => {
      const manager = getStateManager();
      await manager.getState();

      const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
      const badListener = () => {
        throw new Error('Listener failed');
      };
      const goodListener = vi.fn();

      manager.addListener(badListener);
      manager.addListener(goodListener);

      await manager.toggleEnabled();

      expect(goodListener).toHaveBeenCalled();
      expect(errorSpy).toHaveBeenCalled();

      errorSpy.mockRestore();
    });
  });

  describe('singleton', () => {
    it('getStateManager returns same instance', () => {
      const manager1 = getStateManager();
      const manager2 = getStateManager();

      expect(manager1).toBe(manager2);
    });

    it('resetStateManager creates new instance', async () => {
      const manager1 = getStateManager();
      await manager1.getState();

      resetStateManager();

      const manager2 = getStateManager();
      expect(manager1).not.toBe(manager2);
    });
  });
});
