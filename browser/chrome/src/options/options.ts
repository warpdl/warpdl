/**
 * Options page logic.
 * Handles settings configuration and persistence.
 */

import { loadSettings, saveSettings, resetSettings, DEFAULT_SETTINGS } from '@shared/storage';
import type { ExtensionSettings } from '@shared/types';

/**
 * UI element references.
 */
interface UIElements {
  sizeThreshold: HTMLInputElement;
  sizeThresholdValue: HTMLElement;
  downloadDirectory: HTMLInputElement;
  maxConnections: HTMLInputElement;
  maxConnectionsValue: HTMLElement;
  maxSegments: HTMLInputElement;
  maxSegmentsValue: HTMLElement;
  domainList: HTMLElement;
  newDomain: HTMLInputElement;
  addDomainBtn: HTMLButtonElement;
  saveBtn: HTMLButtonElement;
  resetBtn: HTMLButtonElement;
  statusMessage: HTMLElement;
}

/**
 * Current settings state.
 */
let currentSettings: ExtensionSettings;

/**
 * Gets UI element references.
 */
function getElements(): UIElements {
  return {
    sizeThreshold: document.getElementById('size-threshold') as HTMLInputElement,
    sizeThresholdValue: document.getElementById('size-threshold-value')!,
    downloadDirectory: document.getElementById('download-directory') as HTMLInputElement,
    maxConnections: document.getElementById('max-connections') as HTMLInputElement,
    maxConnectionsValue: document.getElementById('max-connections-value')!,
    maxSegments: document.getElementById('max-segments') as HTMLInputElement,
    maxSegmentsValue: document.getElementById('max-segments-value')!,
    domainList: document.getElementById('domain-list')!,
    newDomain: document.getElementById('new-domain') as HTMLInputElement,
    addDomainBtn: document.getElementById('add-domain-btn') as HTMLButtonElement,
    saveBtn: document.getElementById('save-btn') as HTMLButtonElement,
    resetBtn: document.getElementById('reset-btn') as HTMLButtonElement,
    statusMessage: document.getElementById('status-message')!,
  };
}

/**
 * Converts bytes to MB.
 */
function bytesToMB(bytes: number): number {
  return Math.round(bytes / (1024 * 1024));
}

/**
 * Converts MB to bytes.
 */
function mbToBytes(mb: number): number {
  return mb * 1024 * 1024;
}

/**
 * Formats size threshold display.
 */
function formatSizeThreshold(mb: number): string {
  if (mb >= 1024) {
    return `${(mb / 1024).toFixed(1)} GB`;
  }
  return `${mb} MB`;
}

/**
 * Updates the UI to reflect current settings.
 */
function updateUI(elements: UIElements, settings: ExtensionSettings): void {
  // Size threshold (stored in bytes, displayed in MB)
  const sizeInMB = bytesToMB(settings.sizeThreshold);
  elements.sizeThreshold.value = String(sizeInMB);
  elements.sizeThresholdValue.textContent = formatSizeThreshold(sizeInMB);

  // Download directory
  elements.downloadDirectory.value = settings.downloadDirectory;

  // Max connections
  elements.maxConnections.value = String(settings.maxConnections);
  elements.maxConnectionsValue.textContent = String(settings.maxConnections);

  // Max segments
  elements.maxSegments.value = String(settings.maxSegments);
  elements.maxSegmentsValue.textContent = String(settings.maxSegments);

  // Domain list
  renderDomainList(elements.domainList, settings.excludedDomains);
}

/**
 * Renders the domain exclusion list.
 */
function renderDomainList(container: HTMLElement, domains: string[]): void {
  if (domains.length === 0) {
    container.innerHTML = '<div class="empty-state">No excluded domains</div>';
    return;
  }

  container.innerHTML = domains
    .map(
      (domain) => `
      <div class="domain-item" data-domain="${domain}">
        <span class="domain-name">${domain}</span>
        <button class="remove-domain" data-domain="${domain}">Remove</button>
      </div>
    `
    )
    .join('');
}

/**
 * Gets current settings from UI.
 */
function getSettingsFromUI(elements: UIElements): Partial<ExtensionSettings> {
  return {
    sizeThreshold: mbToBytes(parseInt(elements.sizeThreshold.value, 10)),
    downloadDirectory: elements.downloadDirectory.value.trim(),
    maxConnections: parseInt(elements.maxConnections.value, 10),
    maxSegments: parseInt(elements.maxSegments.value, 10),
    excludedDomains: currentSettings.excludedDomains,
  };
}

/**
 * Shows a status message.
 */
function showStatus(elements: UIElements, message: string, type: 'success' | 'error'): void {
  elements.statusMessage.textContent = message;
  elements.statusMessage.className = `status-message ${type}`;

  // Auto-hide after 3 seconds
  setTimeout(() => {
    elements.statusMessage.className = 'status-message hidden';
  }, 3000);
}

/**
 * Validates a domain string.
 */
function isValidDomain(domain: string): boolean {
  // Simple domain validation
  const domainRegex = /^[a-zA-Z0-9][a-zA-Z0-9-]*(\.[a-zA-Z0-9][a-zA-Z0-9-]*)*$/;
  return domainRegex.test(domain);
}

/**
 * Adds a domain to the exclusion list.
 */
function addDomain(elements: UIElements, domain: string): void {
  const normalizedDomain = domain.toLowerCase().trim();

  if (!normalizedDomain) {
    return;
  }

  if (!isValidDomain(normalizedDomain)) {
    showStatus(elements, 'Invalid domain format', 'error');
    return;
  }

  if (currentSettings.excludedDomains.includes(normalizedDomain)) {
    showStatus(elements, 'Domain already excluded', 'error');
    return;
  }

  currentSettings.excludedDomains.push(normalizedDomain);
  renderDomainList(elements.domainList, currentSettings.excludedDomains);
  elements.newDomain.value = '';
}

/**
 * Removes a domain from the exclusion list.
 */
function removeDomain(elements: UIElements, domain: string): void {
  const index = currentSettings.excludedDomains.indexOf(domain);
  if (index !== -1) {
    currentSettings.excludedDomains.splice(index, 1);
    renderDomainList(elements.domainList, currentSettings.excludedDomains);
  }
}

/**
 * Saves current settings.
 */
async function saveCurrentSettings(elements: UIElements): Promise<void> {
  try {
    const settings = getSettingsFromUI(elements);
    await saveSettings(settings);
    currentSettings = { ...currentSettings, ...settings };
    showStatus(elements, 'Settings saved', 'success');
  } catch (err) {
    console.error('[WarpDL Options] Failed to save settings:', err);
    showStatus(elements, 'Failed to save settings', 'error');
  }
}

/**
 * Resets settings to defaults.
 */
async function resetToDefaults(elements: UIElements): Promise<void> {
  try {
    await resetSettings();
    currentSettings = { ...DEFAULT_SETTINGS };
    updateUI(elements, currentSettings);
    showStatus(elements, 'Settings reset to defaults', 'success');
  } catch (err) {
    console.error('[WarpDL Options] Failed to reset settings:', err);
    showStatus(elements, 'Failed to reset settings', 'error');
  }
}

/**
 * Sets up event listeners for slider value display updates.
 */
function setupSliderListeners(elements: UIElements): void {
  elements.sizeThreshold.addEventListener('input', () => {
    const value = parseInt(elements.sizeThreshold.value, 10);
    elements.sizeThresholdValue.textContent = formatSizeThreshold(value);
  });

  elements.maxConnections.addEventListener('input', () => {
    elements.maxConnectionsValue.textContent = elements.maxConnections.value;
  });

  elements.maxSegments.addEventListener('input', () => {
    elements.maxSegmentsValue.textContent = elements.maxSegments.value;
  });
}

/**
 * Sets up domain list event listeners.
 */
function setupDomainListeners(elements: UIElements): void {
  // Add domain button
  elements.addDomainBtn.addEventListener('click', () => {
    addDomain(elements, elements.newDomain.value);
  });

  // Enter key in domain input
  elements.newDomain.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      addDomain(elements, elements.newDomain.value);
    }
  });

  // Remove domain button (delegated)
  elements.domainList.addEventListener('click', (e) => {
    const target = e.target as HTMLElement;
    if (target.classList.contains('remove-domain')) {
      const domain = target.dataset['domain'];
      if (domain) {
        removeDomain(elements, domain);
      }
    }
  });
}

/**
 * Initializes the options page.
 */
async function init(): Promise<void> {
  const elements = getElements();

  // Load current settings
  currentSettings = await loadSettings();
  updateUI(elements, currentSettings);

  // Set up event listeners
  setupSliderListeners(elements);
  setupDomainListeners(elements);

  // Save button
  elements.saveBtn.addEventListener('click', () => {
    saveCurrentSettings(elements);
  });

  // Reset button
  elements.resetBtn.addEventListener('click', () => {
    if (confirm('Reset all settings to defaults?')) {
      resetToDefaults(elements);
    }
  });
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', init);
