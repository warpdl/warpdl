/**
 * Native messaging client integration tests.
 * Tests connection lifecycle, request/response handling, timeouts, and reconnection.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { resetMockChrome, getMockPort, setLastError } from '../mocks/chrome-api';
import { createMockNativeHost, MockNativeHost } from '../mocks/native-host';
import {
  NativeMessagingClient,
  getClient,
  resetClient,
  type ConnectionState,
} from '@background/native-messaging';

describe('NativeMessagingClient', () => {
  let client: NativeMessagingClient;
  let mockHost: MockNativeHost;

  beforeEach(() => {
    resetMockChrome();
    vi.useFakeTimers();
    client = new NativeMessagingClient({ timeout: 1000 });
  });

  afterEach(() => {
    client.disconnect();
    vi.useRealTimers();
  });

  describe('connection lifecycle', () => {
    it('starts in disconnected state', () => {
      expect(client.getState()).toBe('disconnected');
      expect(client.isConnected()).toBe(false);
    });

    it('connects successfully', async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      expect(client.getState()).toBe('connected');
      expect(client.isConnected()).toBe(true);
    });

    it('notifies state listeners on connect', async () => {
      const states: ConnectionState[] = [];
      client.addStateListener((state) => states.push(state));

      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      expect(states).toContain('connecting');
      expect(states).toContain('connected');
    });

    it('disconnects gracefully', async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      client.disconnect();

      expect(client.getState()).toBe('disconnected');
      expect(client.isConnected()).toBe(false);
    });

    it('rejects duplicate connection attempts', async () => {
      const firstConnect = client.connect();
      await vi.runAllTimersAsync();
      await firstConnect;

      // Already connected - should resolve immediately
      const secondConnect = client.connect();
      await vi.runAllTimersAsync();
      await secondConnect;

      expect(client.isConnected()).toBe(true);
    });

    it('handles connection errors', async () => {
      setLastError({ message: 'Native host not found' });

      await expect(client.connect()).rejects.toThrow('Native host not found');
      expect(client.getState()).toBe('disconnected');
    });

    it('removes state listeners', async () => {
      const states: ConnectionState[] = [];
      const listener = (state: ConnectionState) => states.push(state);

      client.addStateListener(listener);
      client.removeStateListener(listener);

      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      // Should not have received any state updates after removal
      expect(states).toHaveLength(0);
    });
  });

  describe('request/response correlation', () => {
    beforeEach(async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      mockHost = createMockNativeHost();
      mockHost.attach();
    });

    it('correlates responses by ID', async () => {
      const versionPromise = client.version();
      await vi.runAllTimersAsync();
      const result = await versionPromise;

      expect(result).toHaveProperty('version');
    });

    it('handles multiple concurrent requests', async () => {
      const promise1 = client.version();
      const promise2 = client.list();
      await vi.runAllTimersAsync();

      const [result1, result2] = await Promise.all([promise1, promise2]);

      expect(result1).toHaveProperty('version');
      expect(result2).toHaveProperty('downloads');
    });

    it('ignores responses for unknown request IDs', async () => {
      const port = getMockPort()!;
      const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

      // Simulate response for non-existent request
      port._simulateMessage({ id: 9999, ok: true, result: {} });

      expect(consoleSpy).toHaveBeenCalledWith(
        'Received response for unknown request:',
        9999
      );

      consoleSpy.mockRestore();
    });

    it('logs warning for invalid response format', async () => {
      const port = getMockPort()!;
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

      // Simulate invalid response
      port._simulateMessage({ invalid: 'response' });

      expect(consoleSpy).toHaveBeenCalledWith(
        'Invalid response from native host:',
        { invalid: 'response' }
      );

      consoleSpy.mockRestore();
    });
  });

  describe('timeout handling', () => {
    beforeEach(async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      // Create mock that doesn't auto-respond
      mockHost = createMockNativeHost({ autoRespond: false });
      mockHost.attach();
    });

    it('times out requests after configured duration', async () => {
      const versionPromise = client.version();

      // Advance past timeout
      vi.advanceTimersByTime(1500);

      await expect(versionPromise).rejects.toThrow('Request timed out after 1000ms');
    });

    it('cleans up pending request on timeout', async () => {
      const versionPromise = client.version();
      vi.advanceTimersByTime(1500);

      await expect(versionPromise).rejects.toThrow('Request timed out');

      // Subsequent request should get new ID and work
      const newMockHost = createMockNativeHost({ autoRespond: true });
      newMockHost.attach();

      const secondPromise = client.version();
      await vi.runAllTimersAsync();
      const result = await secondPromise;

      expect(result).toHaveProperty('version');
    });
  });

  describe('error handling', () => {
    it('throws when not connected', async () => {
      // Client starts disconnected
      await expect(client.version()).rejects.toThrow('Not connected to native host');
    });

    it('propagates daemon errors', async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      mockHost = createMockNativeHost();
      mockHost.attach();

      mockHost.setHandler('download', (req) => ({
        id: req.id,
        ok: false,
        error: 'Download failed: invalid URL',
      }));

      await expect(
        client.download({ url: 'invalid' })
      ).rejects.toThrow('Download failed: invalid URL');
    });

    it('rejects pending requests on disconnect', async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      // Create a mock host that doesn't auto-respond
      mockHost = createMockNativeHost({ autoRespond: false });
      mockHost.attach();

      const versionPromise = client.version();

      // Disconnect immediately before any response
      client.disconnect();

      await expect(versionPromise).rejects.toThrow('Disconnected');
    });
  });

  describe('API methods', () => {
    beforeEach(async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      mockHost = createMockNativeHost();
      mockHost.attach();
    });

    it('version() returns version info', async () => {
      const versionPromise = client.version();
      await vi.runAllTimersAsync();
      const result = await versionPromise;

      expect(result).toHaveProperty('version', '1.0.0');
      expect(result).toHaveProperty('commit');
    });

    it('download() initiates download', async () => {
      const downloadPromise = client.download({
        url: 'https://example.com/file.zip',
        fileName: 'custom.zip',
      });
      await vi.runAllTimersAsync();
      const result = await downloadPromise;

      expect(result).toHaveProperty('downloadId');
      expect(result).toHaveProperty('fileName', 'custom.zip');
      expect(result).toHaveProperty('totalSize');
    });

    it('download() validates params', async () => {
      await expect(
        client.download({} as never)
      ).rejects.toThrow('url is required');
    });

    it('list() returns downloads', async () => {
      const listPromise = client.list();
      await vi.runAllTimersAsync();
      const result = await listPromise;

      expect(result).toHaveProperty('downloads');
      expect(Array.isArray(result.downloads)).toBe(true);
    });

    it('list() accepts optional params', async () => {
      const listPromise = client.list({ includeHidden: true, includeMetadata: true });
      await vi.runAllTimersAsync();
      const result = await listPromise;

      expect(result).toHaveProperty('downloads');
    });

    it('stop() stops download', async () => {
      const stopPromise = client.stop({ downloadId: 'dl-1' });
      await vi.runAllTimersAsync();
      const result = await stopPromise;

      expect(result).toHaveProperty('success', true);
    });

    it('stop() validates params', async () => {
      await expect(
        client.stop({} as never)
      ).rejects.toThrow('downloadId is required');
    });

    it('resume() resumes download', async () => {
      const resumePromise = client.resume({ downloadId: 'dl-1' });
      await vi.runAllTimersAsync();
      const result = await resumePromise;

      expect(result).toHaveProperty('downloadId', 'dl-1');
      expect(result).toHaveProperty('fileName');
    });

    it('resume() validates params', async () => {
      await expect(
        client.resume({} as never)
      ).rejects.toThrow('downloadId is required');
    });

    it('flush() removes completed download', async () => {
      const flushPromise = client.flush({ downloadId: 'dl-1' });
      await vi.runAllTimersAsync();
      const result = await flushPromise;

      expect(result).toHaveProperty('success', true);
    });

    it('flush() validates params', async () => {
      await expect(
        client.flush({} as never)
      ).rejects.toThrow('downloadId is required');
    });
  });

  describe('reconnection', () => {
    it('attempts reconnection on disconnect', async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      const states: ConnectionState[] = [];
      client.addStateListener((state) => states.push(state));

      // Simulate disconnect
      const port = getMockPort()!;
      port._simulateDisconnect();

      // Should be reconnecting
      expect(states).toContain('reconnecting');
    });

    it('gives up after max attempts', async () => {
      const connectPromise = client.connect();
      await vi.runAllTimersAsync();
      await connectPromise;

      // Make reconnection fail
      setLastError({ message: 'Host unavailable' });

      const states: ConnectionState[] = [];
      client.addStateListener((state) => states.push(state));

      // Simulate disconnect
      const port = getMockPort()!;
      port._simulateDisconnect();

      // Run through all reconnection attempts
      for (let i = 0; i < 5; i++) {
        await vi.advanceTimersByTimeAsync(10000);
      }

      expect(states).toContain('disconnected');
    });
  });
});

describe('singleton client', () => {
  beforeEach(() => {
    resetMockChrome();
    resetClient();
  });

  afterEach(() => {
    resetClient();
  });

  it('getClient() returns same instance', () => {
    const client1 = getClient();
    const client2 = getClient();

    expect(client1).toBe(client2);
  });

  it('resetClient() creates new instance', () => {
    const client1 = getClient();
    resetClient();
    const client2 = getClient();

    expect(client1).not.toBe(client2);
  });
});
