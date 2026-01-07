/**
 * Native host mock for testing.
 * Simulates responses from the WarpDL native messaging host.
 */

import { vi } from 'vitest';
import type {
  NativeRequest,
  NativeResponse,
  VersionResponse,
  DownloadResponse,
  ResumeResponse,
  ListResponse,
  SuccessResponse,
} from '@shared/types';
import { getMockPort, type MockPort } from './chrome-api';

/**
 * Mock response generator interface.
 */
export interface MockResponseHandler {
  (request: NativeRequest): NativeResponse | Promise<NativeResponse>;
}

/**
 * Default mock responses for each method.
 */
export const defaultMockResponses: Record<string, (id: number, message?: Record<string, unknown>) => NativeResponse> = {
  version: (id) => ({
    id,
    ok: true,
    result: {
      version: '1.0.0',
      commit: 'abc123',
      buildDate: '2024-01-01',
    } satisfies VersionResponse,
  }),

  download: (id, message) => ({
    id,
    ok: true,
    result: {
      downloadId: 'dl-' + Date.now(),
      fileName: (message?.['fileName'] as string) || 'file.zip',
      totalSize: 1024 * 1024 * 10, // 10MB
    } satisfies DownloadResponse,
  }),

  list: (id) => ({
    id,
    ok: true,
    result: {
      downloads: [
        {
          id: 'dl-1',
          url: 'https://example.com/file1.zip',
          fileName: 'file1.zip',
          downloadDirectory: '/downloads',
          totalSize: 1024 * 1024,
          downloadedSize: 512 * 1024,
          status: 'downloading',
          createdAt: '2024-01-01T00:00:00Z',
          updatedAt: '2024-01-01T00:01:00Z',
        },
      ],
    } satisfies ListResponse,
  }),

  stop: (id) => ({
    id,
    ok: true,
    result: { success: true } satisfies SuccessResponse,
  }),

  resume: (id, message) => ({
    id,
    ok: true,
    result: {
      downloadId: (message?.['downloadId'] as string) || 'dl-1',
      fileName: 'file.zip',
    } satisfies ResumeResponse,
  }),

  flush: (id) => ({
    id,
    ok: true,
    result: { success: true } satisfies SuccessResponse,
  }),
};

/**
 * Configuration for the mock native host.
 */
export interface MockNativeHostConfig {
  /** Delay before sending responses (ms) */
  responseDelay?: number;
  /** Custom response handlers by method */
  handlers?: Partial<Record<string, MockResponseHandler>>;
  /** Whether to auto-respond to messages */
  autoRespond?: boolean;
  /** Error to return for all requests */
  globalError?: string;
}

/**
 * Mock native host that auto-responds to messages.
 */
export class MockNativeHost {
  private config: MockNativeHostConfig;
  private port: MockPort | null = null;
  private pendingRequests: Map<number, NativeRequest> = new Map();

  constructor(config: MockNativeHostConfig = {}) {
    this.config = {
      responseDelay: 0,
      autoRespond: true,
      ...config,
    };
  }

  /**
   * Attaches to the current mock port and sets up auto-response.
   */
  attach(): void {
    this.port = getMockPort();
    if (!this.port) {
      throw new Error('No mock port available. Call chrome.runtime.connectNative first.');
    }

    // Intercept postMessage to capture requests
    const originalPostMessage = this.port.postMessage;
    this.port.postMessage = vi.fn((message: unknown) => {
      originalPostMessage(message);
      const request = message as NativeRequest;
      this.pendingRequests.set(request.id, request);

      if (this.config.autoRespond) {
        this.respond(request);
      }
    });
  }

  /**
   * Detaches from the port.
   */
  detach(): void {
    this.port = null;
    this.pendingRequests.clear();
  }

  /**
   * Sends a response for a request.
   */
  async respond(request: NativeRequest): Promise<void> {
    if (!this.port) return;

    const delay = this.config.responseDelay ?? 0;
    if (delay > 0) {
      await new Promise((resolve) => setTimeout(resolve, delay));
    }

    let response: NativeResponse;

    if (this.config.globalError) {
      response = {
        id: request.id,
        ok: false,
        error: this.config.globalError,
      };
    } else if (this.config.handlers?.[request.method]) {
      const handler = this.config.handlers[request.method];
      response = await handler!(request);
    } else if (defaultMockResponses[request.method]) {
      response = defaultMockResponses[request.method]!(request.id, request.message);
    } else {
      response = {
        id: request.id,
        ok: false,
        error: `Unknown method: ${request.method}`,
      };
    }

    this.port._simulateMessage(response);
    this.pendingRequests.delete(request.id);
  }

  /**
   * Simulates a disconnect event.
   */
  simulateDisconnect(): void {
    this.port?._simulateDisconnect();
  }

  /**
   * Simulates an error by sending an error response.
   */
  simulateError(requestId: number, error: string): void {
    if (!this.port) return;
    this.port._simulateMessage({
      id: requestId,
      ok: false,
      error,
    });
  }

  /**
   * Gets pending requests that haven't been responded to.
   */
  getPendingRequests(): NativeRequest[] {
    return Array.from(this.pendingRequests.values());
  }

  /**
   * Sets a custom handler for a method.
   */
  setHandler(method: string, handler: MockResponseHandler): void {
    if (!this.config.handlers) {
      this.config.handlers = {};
    }
    this.config.handlers[method] = handler;
  }

  /**
   * Clears all custom handlers.
   */
  clearHandlers(): void {
    this.config.handlers = {};
  }
}

/**
 * Creates a mock native host with default configuration.
 */
export function createMockNativeHost(config?: MockNativeHostConfig): MockNativeHost {
  return new MockNativeHost(config);
}
