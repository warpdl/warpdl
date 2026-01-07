/**
 * Native Messaging Client for communicating with the WarpDL daemon.
 * Implements the Chrome native messaging protocol with request/response correlation,
 * timeout handling, and automatic reconnection.
 */

import {
  NATIVE_HOST_NAME,
  DEFAULT_TIMEOUT_MS,
  MAX_RECONNECT_ATTEMPTS,
  RECONNECT_BASE_DELAY_MS,
} from '@shared/constants';
import {
  createRequest,
  validateDownloadParams,
  validateResumeParams,
  validateStopParams,
  validateFlushParams,
  validateListParams,
  isNativeResponse,
  getResponseError,
} from '@shared/protocol';
import type {
  NativeMethod,
  NativeResponse,
  DownloadParams,
  ResumeParams,
  StopParams,
  FlushParams,
  ListParams,
  VersionResponse,
  DownloadResponse,
  ResumeResponse,
  ListResponse,
  SuccessResponse,
} from '@shared/types';

/**
 * Connection state for the native messaging client.
 */
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

/**
 * Pending request tracking.
 */
interface PendingRequest {
  resolve: (response: NativeResponse) => void;
  reject: (error: Error) => void;
  timeoutId: ReturnType<typeof setTimeout>;
}

/**
 * Event listener type for connection state changes.
 */
export type ConnectionStateListener = (state: ConnectionState) => void;

/**
 * Native messaging client for communicating with the WarpDL daemon.
 */
export class NativeMessagingClient {
  private port: chrome.runtime.Port | null = null;
  private state: ConnectionState = 'disconnected';
  private nextRequestId = 1;
  private pendingRequests: Map<number, PendingRequest> = new Map();
  private reconnectAttempts = 0;
  private stateListeners: Set<ConnectionStateListener> = new Set();
  private defaultTimeout: number;

  constructor(options?: { timeout?: number }) {
    this.defaultTimeout = options?.timeout ?? DEFAULT_TIMEOUT_MS;
  }

  /**
   * Gets the current connection state.
   */
  getState(): ConnectionState {
    return this.state;
  }

  /**
   * Checks if the client is connected.
   */
  isConnected(): boolean {
    return this.state === 'connected';
  }

  /**
   * Adds a listener for connection state changes.
   */
  addStateListener(listener: ConnectionStateListener): void {
    this.stateListeners.add(listener);
  }

  /**
   * Removes a connection state listener.
   */
  removeStateListener(listener: ConnectionStateListener): void {
    this.stateListeners.delete(listener);
  }

  /**
   * Notifies all state listeners of a state change.
   */
  private setState(newState: ConnectionState): void {
    if (this.state !== newState) {
      this.state = newState;
      this.stateListeners.forEach((listener) => listener(newState));
    }
  }

  /**
   * Connects to the native messaging host.
   * @returns Promise that resolves when connected or rejects on failure
   */
  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      if (this.state === 'connected') {
        resolve();
        return;
      }

      if (this.state === 'connecting' || this.state === 'reconnecting') {
        reject(new Error('Connection already in progress'));
        return;
      }

      this.setState('connecting');

      try {
        this.port = chrome.runtime.connectNative(NATIVE_HOST_NAME);

        // Set up message handler
        this.port.onMessage.addListener(this.handleMessage);

        // Set up disconnect handler
        this.port.onDisconnect.addListener(() => {
          this.handleDisconnect();
        });

        // Check for immediate connection error
        if (chrome.runtime.lastError) {
          const error = new Error(chrome.runtime.lastError.message ?? 'Connection failed');
          this.setState('disconnected');
          reject(error);
          return;
        }

        this.setState('connected');
        this.reconnectAttempts = 0;
        resolve();
      } catch (err) {
        this.setState('disconnected');
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  /**
   * Disconnects from the native messaging host.
   */
  disconnect(): void {
    if (this.port) {
      this.port.disconnect();
      this.port = null;
    }

    // Reject all pending requests
    this.pendingRequests.forEach((pending) => {
      clearTimeout(pending.timeoutId);
      pending.reject(new Error('Disconnected'));
    });
    this.pendingRequests.clear();

    this.setState('disconnected');
  }

  /**
   * Handles incoming messages from the native host.
   */
  private handleMessage = (message: unknown): void => {
    if (!isNativeResponse(message)) {
      console.error('Invalid response from native host:', message);
      return;
    }

    const pending = this.pendingRequests.get(message.id);
    if (!pending) {
      console.warn('Received response for unknown request:', message.id);
      return;
    }

    clearTimeout(pending.timeoutId);
    this.pendingRequests.delete(message.id);
    pending.resolve(message);
  };

  /**
   * Handles disconnect events.
   */
  private handleDisconnect = (): void => {
    this.port = null;

    // Check for error
    const error = chrome.runtime.lastError?.message ?? 'Native host disconnected';

    // Reject all pending requests
    this.pendingRequests.forEach((pending) => {
      clearTimeout(pending.timeoutId);
      pending.reject(new Error(error));
    });
    this.pendingRequests.clear();

    // Attempt reconnection if we were connected
    if (this.state === 'connected' && this.reconnectAttempts < MAX_RECONNECT_ATTEMPTS) {
      this.attemptReconnect();
    } else {
      this.setState('disconnected');
    }
  };

  /**
   * Attempts to reconnect with exponential backoff.
   */
  private async attemptReconnect(): Promise<void> {
    this.setState('reconnecting');
    this.reconnectAttempts++;

    const delay = RECONNECT_BASE_DELAY_MS * Math.pow(2, this.reconnectAttempts - 1);
    await new Promise((resolve) => setTimeout(resolve, delay));

    try {
      await this.connect();
    } catch {
      if (this.reconnectAttempts < MAX_RECONNECT_ATTEMPTS) {
        this.attemptReconnect();
      } else {
        this.setState('disconnected');
      }
    }
  }

  /**
   * Sends a request to the native host.
   */
  private async sendRequest(
    method: NativeMethod,
    message?: Record<string, unknown>,
    timeout?: number
  ): Promise<NativeResponse> {
    if (!this.port || this.state !== 'connected') {
      throw new Error('Not connected to native host');
    }

    const id = this.nextRequestId++;
    const request = createRequest(id, method, message);
    const timeoutMs = timeout ?? this.defaultTimeout;

    return new Promise((resolve, reject) => {
      const timeoutId = setTimeout(() => {
        this.pendingRequests.delete(id);
        reject(new Error(`Request timed out after ${timeoutMs}ms`));
      }, timeoutMs);

      this.pendingRequests.set(id, { resolve, reject, timeoutId });

      try {
        this.port!.postMessage(request);
      } catch (err) {
        clearTimeout(timeoutId);
        this.pendingRequests.delete(id);
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  /**
   * Extracts result from response, throwing on error.
   */
  private extractResult<T>(response: NativeResponse): T {
    const error = getResponseError(response);
    if (error) {
      throw new Error(error);
    }
    return response.result as T;
  }

  /**
   * Gets the daemon version.
   */
  async version(): Promise<VersionResponse> {
    const response = await this.sendRequest('version');
    return this.extractResult<VersionResponse>(response);
  }

  /**
   * Initiates a download.
   */
  async download(params: DownloadParams): Promise<DownloadResponse> {
    validateDownloadParams(params);
    const response = await this.sendRequest('download', params as unknown as Record<string, unknown>);
    return this.extractResult<DownloadResponse>(response);
  }

  /**
   * Lists downloads.
   */
  async list(params?: ListParams): Promise<ListResponse> {
    if (params) {
      validateListParams(params);
    }
    const response = await this.sendRequest('list', params as unknown as Record<string, unknown>);
    return this.extractResult<ListResponse>(response);
  }

  /**
   * Stops a download.
   */
  async stop(params: StopParams): Promise<SuccessResponse> {
    validateStopParams(params);
    const response = await this.sendRequest('stop', params as unknown as Record<string, unknown>);
    return this.extractResult<SuccessResponse>(response);
  }

  /**
   * Resumes a download.
   */
  async resume(params: ResumeParams): Promise<ResumeResponse> {
    validateResumeParams(params);
    const response = await this.sendRequest('resume', params as unknown as Record<string, unknown>);
    return this.extractResult<ResumeResponse>(response);
  }

  /**
   * Flushes a completed download from the manager.
   */
  async flush(params: FlushParams): Promise<SuccessResponse> {
    validateFlushParams(params);
    const response = await this.sendRequest('flush', params as unknown as Record<string, unknown>);
    return this.extractResult<SuccessResponse>(response);
  }
}

/**
 * Singleton instance of the native messaging client.
 */
let clientInstance: NativeMessagingClient | null = null;

/**
 * Gets the singleton native messaging client instance.
 */
export function getClient(): NativeMessagingClient {
  if (!clientInstance) {
    clientInstance = new NativeMessagingClient();
  }
  return clientInstance;
}

/**
 * Resets the singleton client (for testing).
 */
export function resetClient(): void {
  if (clientInstance) {
    clientInstance.disconnect();
  }
  clientInstance = null;
}
