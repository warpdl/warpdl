/**
 * Chrome API mocks for testing.
 * Sets up a mock chrome object that mimics the Chrome extension APIs.
 */

import { vi } from 'vitest';

// Storage mock data
let syncStorage: Record<string, unknown> = {};
let sessionStorage: Record<string, unknown> = {};
let localStorage: Record<string, unknown> = {};

// Port mock for native messaging
export interface MockPort {
  name: string;
  onMessage: {
    addListener: ReturnType<typeof vi.fn>;
    removeListener: ReturnType<typeof vi.fn>;
    hasListeners: () => boolean;
  };
  onDisconnect: {
    addListener: ReturnType<typeof vi.fn>;
    removeListener: ReturnType<typeof vi.fn>;
    hasListeners: () => boolean;
  };
  postMessage: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;
  _messageListeners: ((message: unknown) => void)[];
  _disconnectListeners: (() => void)[];
  _simulateMessage: (message: unknown) => void;
  _simulateDisconnect: () => void;
}

export function createMockPort(name: string): MockPort {
  const messageListeners: ((message: unknown) => void)[] = [];
  const disconnectListeners: (() => void)[] = [];

  return {
    name,
    onMessage: {
      addListener: vi.fn((listener: (message: unknown) => void) => {
        messageListeners.push(listener);
      }),
      removeListener: vi.fn((listener: (message: unknown) => void) => {
        const index = messageListeners.indexOf(listener);
        if (index > -1) messageListeners.splice(index, 1);
      }),
      hasListeners: () => messageListeners.length > 0,
    },
    onDisconnect: {
      addListener: vi.fn((listener: () => void) => {
        disconnectListeners.push(listener);
      }),
      removeListener: vi.fn((listener: () => void) => {
        const index = disconnectListeners.indexOf(listener);
        if (index > -1) disconnectListeners.splice(index, 1);
      }),
      hasListeners: () => disconnectListeners.length > 0,
    },
    postMessage: vi.fn(),
    disconnect: vi.fn(),
    _messageListeners: messageListeners,
    _disconnectListeners: disconnectListeners,
    _simulateMessage: (message: unknown) => {
      messageListeners.forEach((listener) => listener(message));
    },
    _simulateDisconnect: () => {
      disconnectListeners.forEach((listener) => listener());
    },
  };
}

// Create a reusable mock port for native messaging
let currentMockPort: MockPort | null = null;

export function getMockPort(): MockPort | null {
  return currentMockPort;
}

export function setMockPort(port: MockPort | null): void {
  currentMockPort = port;
}

// Mock chrome object
const mockChrome = {
  runtime: {
    connectNative: vi.fn((hostName: string) => {
      currentMockPort = createMockPort(hostName);
      return currentMockPort;
    }),
    lastError: null as { message: string } | null,
    onInstalled: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
    onStartup: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
    onMessage: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
    sendMessage: vi.fn(() => Promise.resolve()),
    getManifest: vi.fn(() => ({
      name: 'WarpDL',
      version: '1.0.0',
    })),
  },
  storage: {
    onChanged: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
    sync: {
      get: vi.fn(async (keys: string[] | string | null) => {
        if (keys === null) return { ...syncStorage };
        const keyArr = Array.isArray(keys) ? keys : [keys];
        const result: Record<string, unknown> = {};
        keyArr.forEach((key) => {
          if (key in syncStorage) {
            result[key] = syncStorage[key];
          }
        });
        return result;
      }),
      set: vi.fn(async (items: Record<string, unknown>) => {
        Object.assign(syncStorage, items);
      }),
      remove: vi.fn(async (keys: string[] | string) => {
        const keyArr = Array.isArray(keys) ? keys : [keys];
        keyArr.forEach((key) => {
          delete syncStorage[key];
        });
      }),
      clear: vi.fn(async () => {
        syncStorage = {};
      }),
      onChanged: {
        addListener: vi.fn(),
        removeListener: vi.fn(),
      },
    },
    session: {
      get: vi.fn(async (keys: string[] | string | null) => {
        if (keys === null) return { ...sessionStorage };
        const keyArr = Array.isArray(keys) ? keys : [keys];
        const result: Record<string, unknown> = {};
        keyArr.forEach((key) => {
          if (key in sessionStorage) {
            result[key] = sessionStorage[key];
          }
        });
        return result;
      }),
      set: vi.fn(async (items: Record<string, unknown>) => {
        Object.assign(sessionStorage, items);
      }),
      remove: vi.fn(async (keys: string[] | string) => {
        const keyArr = Array.isArray(keys) ? keys : [keys];
        keyArr.forEach((key) => {
          delete sessionStorage[key];
        });
      }),
      clear: vi.fn(async () => {
        sessionStorage = {};
      }),
      onChanged: {
        addListener: vi.fn(),
        removeListener: vi.fn(),
      },
    },
    local: {
      get: vi.fn(async (keys: string[] | string | null) => {
        if (keys === null) return { ...localStorage };
        const keyArr = Array.isArray(keys) ? keys : [keys];
        const result: Record<string, unknown> = {};
        keyArr.forEach((key) => {
          if (key in localStorage) {
            result[key] = localStorage[key];
          }
        });
        return result;
      }),
      set: vi.fn(async (items: Record<string, unknown>) => {
        Object.assign(localStorage, items);
      }),
      remove: vi.fn(async (keys: string[] | string) => {
        const keyArr = Array.isArray(keys) ? keys : [keys];
        keyArr.forEach((key) => {
          delete localStorage[key];
        });
      }),
      clear: vi.fn(async () => {
        localStorage = {};
      }),
      onChanged: {
        addListener: vi.fn(),
        removeListener: vi.fn(),
      },
    },
  },
  downloads: {
    onDeterminingFilename: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
    cancel: vi.fn(async () => {}),
    download: vi.fn(async () => 1),
  },
  contextMenus: {
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    removeAll: vi.fn(async () => {}),
    onClicked: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
  },
  alarms: {
    create: vi.fn(),
    clear: vi.fn(async () => true),
    get: vi.fn(async () => null),
    getAll: vi.fn(async () => []),
    onAlarm: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
  },
  notifications: {
    create: vi.fn(async () => 'notification-id'),
    clear: vi.fn(async () => true),
    onClicked: {
      addListener: vi.fn(),
      removeListener: vi.fn(),
    },
  },
};

// Reset function for tests
export function resetMockChrome(): void {
  syncStorage = {};
  sessionStorage = {};
  localStorage = {};
  currentMockPort = null;
  mockChrome.runtime.lastError = null;

  // Reset all mocks
  vi.clearAllMocks();
}

// Set up mock storage with initial values
export function setMockSyncStorage(data: Record<string, unknown>): void {
  syncStorage = { ...data };
}

export function setMockSessionStorage(data: Record<string, unknown>): void {
  sessionStorage = { ...data };
}

export function setMockLocalStorage(data: Record<string, unknown>): void {
  localStorage = { ...data };
}

export function getMockSyncStorage(): Record<string, unknown> {
  return { ...syncStorage };
}

export function getMockSessionStorage(): Record<string, unknown> {
  return { ...sessionStorage };
}

// Make lastError settable for testing error scenarios
export function setLastError(error: { message: string } | null): void {
  mockChrome.runtime.lastError = error;
}

// Install the mock globally
(globalThis as unknown as { chrome: typeof mockChrome }).chrome = mockChrome;

// Export for direct access in tests
export { mockChrome };
