# WarpDL Chrome Extension

A Manifest V3 Chrome extension that intercepts browser downloads and redirects them to the WarpDL daemon for accelerated parallel downloading.

## Features

- **Automatic Download Interception**: Downloads above the configured size threshold are automatically handled by WarpDL
- **Context Menu Integration**: Right-click any link, image, video, or audio to download with WarpDL
- **Configurable Settings**: Customize size threshold, excluded domains, max connections, and segments
- **Connection Status**: Real-time status display in the popup
- **Graceful Fallback**: Falls back to Chrome's native download if daemon is unavailable

## Installation

### Prerequisites

1. WarpDL must be installed and the daemon running:
   ```bash
   warpdl daemon start
   ```

### Build from Source

1. Install dependencies:
   ```bash
   cd browser/chrome
   bun install
   ```

2. Build the extension:
   ```bash
   bun run build
   ```

3. Load in Chrome:
   - Open `chrome://extensions`
   - Enable "Developer mode"
   - Click "Load unpacked"
   - Select the `browser/chrome/dist` directory
   - Note the Extension ID

4. Install native messaging host:
   ```bash
   warpdl native-host install --chrome-extension-id=<YOUR_EXTENSION_ID>
   ```

## Usage

### Automatic Interception

Once installed and connected, downloads above the size threshold (default: 1MB) are automatically intercepted and handled by WarpDL.

### Context Menu

Right-click on any:
- **Link**: Select "Download with WarpDL"
- **Image**: Select "Download with WarpDL"
- **Video**: Select "Download with WarpDL"
- **Audio**: Select "Download with WarpDL"

### Popup

Click the WarpDL icon in the toolbar to:
- View connection status
- See active download count
- Toggle download interception on/off
- Access settings

### Settings

Access via popup "Settings" link or `chrome://extensions` → WarpDL Options:

- **File Size Threshold**: Only intercept files larger than this size (1MB - 100MB)
- **Download Directory**: Default directory for downloads
- **Max Connections**: Maximum concurrent connections per download (1-32)
- **Max Segments**: Maximum segments per download (1-64)
- **Excluded Domains**: List of domains to ignore (downloads from these pass through to Chrome)

## Development

### Commands

```bash
# Install dependencies
bun install

# Build for production
bun run build

# Development build with watch
bun run dev

# Run tests
bun run test

# Run tests with coverage
bun run test:coverage

# Type check
bun run typecheck
```

### Project Structure

```
browser/chrome/
├── manifest.json           # Chrome extension manifest
├── src/
│   ├── background/         # Service worker and core logic
│   │   ├── service-worker.ts
│   │   ├── native-messaging.ts
│   │   ├── download-interceptor.ts
│   │   ├── context-menu.ts
│   │   └── state.ts
│   ├── popup/              # Popup UI
│   ├── options/            # Options page
│   ├── shared/             # Shared types, utilities, constants
│   └── assets/             # Icons and static assets
├── tests/
│   ├── integration/        # Integration tests
│   ├── unit/               # Unit tests
│   └── mocks/              # Chrome API mocks
└── dist/                   # Built extension (load this in Chrome)
```

### Testing

See [TESTING.md](./TESTING.md) for detailed testing instructions.

## Architecture

The extension uses Manifest V3 architecture:

1. **Service Worker** (`service-worker.ts`): Main entry point, initializes components
2. **Native Messaging Client** (`native-messaging.ts`): Communicates with WarpDL daemon via native messaging
3. **Download Interceptor** (`download-interceptor.ts`): Intercepts downloads using `chrome.downloads.onDeterminingFilename`
4. **Context Menu** (`context-menu.ts`): Provides right-click download options
5. **State Manager** (`state.ts`): Manages extension state and settings

### Native Messaging Protocol

Communicates with the WarpDL daemon using:
- Host name: `com.warpdl.host`
- Protocol: 4-byte little-endian length prefix + JSON payload
- Methods: `version`, `download`, `list`, `stop`, `resume`, `flush`

## Troubleshooting

### Extension shows "Disconnected"

1. Verify daemon is running: `warpdl daemon status`
2. Check native host is installed: `warpdl native-host status`
3. Verify extension ID matches in native host manifest

### Downloads not being intercepted

1. Check if interception is enabled (toggle in popup)
2. Verify file size is above threshold
3. Check if domain is in exclusion list
4. Open service worker console for errors

### Native host installation issues

- **macOS**: Manifest at `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/`
- **Linux**: Manifest at `~/.config/google-chrome/NativeMessagingHosts/`
- **Windows**: Registry key `HKCU\Software\Google\Chrome\NativeMessagingHosts\`

## License

Part of WarpDL - see main repository for license information.
