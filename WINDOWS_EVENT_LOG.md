# Windows Event Log Integration

This document describes the Windows Event Log integration for WarpDL Windows Service.

## Overview

When WarpDL runs as a Windows Service, it automatically logs important events to the Windows Event Log. This allows system administrators to monitor the service using standard Windows tools like Event Viewer.

## Event Types

WarpDL logs three types of events:

- **Information (Info)**: Normal service operations
  - Service starting
  - Service started successfully
  - Service stopping
  - Service stopped successfully
  - Download operations

- **Warning**: Recoverable issues
  - Failed to create Event Logger (falls back to console)
  - API cleanup errors during shutdown

- **Error**: Critical failures
  - Failed to start service
  - Service initialization failures
  - Server errors during operation

## Viewing Logs

To view WarpDL service logs in Windows Event Viewer:

1. Open Event Viewer (eventvwr.msc)
2. Navigate to: Windows Logs → Application
3. Look for events with Source: **WarpDL**

## Event Source Registration

The event source is automatically registered/unregistered during service installation/uninstallation:

```bash
# Install service (registers event source)
warpdl service install

# Uninstall service (unregisters event source)
warpdl service uninstall
```

## Console Mode vs Service Mode

- **Console Mode**: When running `warpdl daemon` interactively, logs go to stdout/stderr as usual
- **Service Mode**: When running as a Windows Service, logs go to Windows Event Log

The mode is automatically detected using `svc.IsWindowsService()`.

## Architecture

### Components

1. **EventLogger Interface** (`internal/service/eventlog_windows.go`)
   - Defines the logging interface
   - Two implementations:
     - `WindowsEventLogger`: Uses Windows Event Log
     - `ConsoleEventLogger`: Uses standard Go logging

2. **Service Integration** (`cmd/service_windows.go`)
   - Registers event source on `service install`
   - Unregisters event source on `service uninstall`

3. **Daemon Service Mode** (`cmd/daemon_windows.go`)
   - Detects if running as Windows Service
   - Initializes Windows Event Logger
   - Wraps server logic in service runner
   - Logs all service lifecycle events

### Service Detection

```go
isService, err := svc.IsWindowsService()
if err != nil {
    return false, fmt.Errorf("failed to determine if running as service: %w", err)
}

if isService {
    // Use Windows Event Log
    eventLogger, err := service.NewWindowsEventLogger(serviceName)
    // ...
}
```

## Testing

Run Windows-specific tests:

```bash
# Compile Windows tests on Linux
GOOS=windows go test ./internal/service/... -c

# Run on Windows
go test ./internal/service/... -v
go test ./cmd/... -v
```

## Dependencies

- `golang.org/x/sys/windows/svc/eventlog`: Windows Event Log integration
- `golang.org/x/sys/windows/svc`: Windows Service Control Manager interface

## Known Limitations

- Event Log integration only works on Windows
- Requires administrator privileges to register/unregister event source
- On non-Windows platforms, `checkWindowsService()` always returns false
