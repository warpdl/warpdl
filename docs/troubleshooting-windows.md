# Windows Troubleshooting Guide

This guide helps Windows users resolve common issues with WarpDL related to firewalls, antivirus software, and security prompts.

## Table of Contents

- [Windows Firewall](#windows-firewall)
- [Antivirus Configuration](#antivirus-configuration)
- [Windows SmartScreen](#windows-smartscreen)
- [Common Issues](#common-issues)

## Windows Firewall

### Understanding WarpDL Network Usage

WarpDL uses a daemon (background service) architecture that communicates with the CLI client:

- **Primary Method**: Unix domain sockets (no network access needed)
- **Fallback Method**: TCP on `localhost:3849` when Unix sockets unavailable
- **Web Interface**: TCP on `localhost:3850` (for browser extension integration)

The TCP fallback on port 3849 may trigger a Windows Firewall prompt on first run. The web interface on port 3850 is used for browser extension integration to capture downloads.

### Allow Access in Firewall Prompt

When the firewall prompt appears:

1. **Private networks**: ✅ Check this box (recommended)
2. **Public networks**: ⬜ Leave unchecked (localhost-only, no external access needed)
3. Click **Allow access**

The daemon only listens on `localhost` (127.0.0.1) and never accepts connections from other machines.

### Manual Firewall Rule Configuration

If you dismissed the prompt or need to reconfigure:

#### Option 1: PowerShell (Recommended)

Open PowerShell as Administrator and run:

```powershell
New-NetFirewallRule -DisplayName "WarpDL Daemon" -Direction Inbound -Action Allow -Program "C:\path\to\warpdl.exe" -Profile Private
```

Replace `C:\path\to\warpdl.exe` with your actual installation path:
- **Scoop**: `C:\Users\YourUsername\scoop\apps\warpdl\current\warpdl.exe` (replace `YourUsername` with your Windows username)
- **Manual install**: Path where you placed the binary

**Alternative (netsh for older Windows):**
```powershell
netsh advfirewall firewall add rule name="WarpDL Daemon" dir=in action=allow program="C:\path\to\warpdl.exe" profile=private
```

#### Option 2: Windows Defender Firewall GUI

1. Open **Windows Defender Firewall with Advanced Security**
2. Click **Inbound Rules** → **New Rule...**
3. Select **Program** → Next
4. Browse to `warpdl.exe` → Next
5. Select **Allow the connection** → Next
6. Check **Private** only → Next
7. Name: "WarpDL Daemon" → Finish

### Verify Firewall Configuration

Check if the rule exists:

```powershell
Get-NetFirewallRule -DisplayName "WarpDL Daemon"
```

**Alternative (netsh):**
```powershell
netsh advfirewall firewall show rule name="WarpDL Daemon"
```

## Antivirus Configuration

### Why Download Managers Trigger Antivirus

Download managers may be flagged as suspicious because they:
- Download files from the internet (like browser behavior)
- Use multiple connections simultaneously
- Access network sockets
- Run as background processes

These are **false positives**. WarpDL is open-source and does not contain malware.

### Windows Defender Exclusions

#### Via Settings GUI

1. Open **Windows Security**
2. Go to **Virus & threat protection**
3. Under "Virus & threat protection settings", click **Manage settings**
4. Scroll to **Exclusions** → Click **Add or remove exclusions**
5. Click **Add an exclusion** and add:
   - **Folder**: `%USERPROFILE%\.config\warpdl\` (Config directory - use Windows environment variable format)
   - **Folder**: `%LOCALAPPDATA%\warpdl\` (Application data, if exists)
   - **Process**: `warpdl.exe` (The executable itself)

#### Via PowerShell

Open PowerShell as Administrator:

```powershell
# Add executable exclusion
Add-MpPreference -ExclusionProcess "warpdl.exe"

# Add config directory exclusion (PowerShell uses $env: syntax)
Add-MpPreference -ExclusionPath "$env:USERPROFILE\.config\warpdl"

# Add application data exclusion (if needed)
Add-MpPreference -ExclusionPath "$env:LOCALAPPDATA\warpdl"
```

### Third-Party Antivirus Software

For other antivirus products (Norton, McAfee, Avast, etc.), add exclusions for:

1. **Executable**: The `warpdl.exe` binary
2. **Config folder**: `%USERPROFILE%\.config\warpdl\`
3. **Download folder**: Your designated download directory (optional, if scans interfere)

Consult your antivirus documentation for specific steps:
- **Norton**: Settings → Antivirus → Scans and Risks → Exclusions
- **McAfee**: Settings → Real-Time Scanning → Excluded Files
- **Avast**: Settings → General → Exceptions
- **Bitdefender**: Protection → Antivirus → Exclusions

## Windows SmartScreen

### What is SmartScreen?

Windows SmartScreen is a security feature that warns about unrecognized applications. It may display:

> **Windows protected your PC**  
> Microsoft Defender SmartScreen prevented an unrecognized app from starting.

### Why This Happens

- **Unsigned executables**: Early or development builds may not have code signing certificates
- **Low reputation**: New or infrequently downloaded files trigger warnings
- **Official releases**: Signed binaries should not trigger SmartScreen

### Running Anyway (Safe for WarpDL)

If you trust the source (official releases or self-built binaries):

1. Click **More info** on the SmartScreen dialog
2. Click **Run anyway** button (appears after clicking "More info")

**Note**: Only do this for binaries from trusted sources:
- Official releases from [github.com/warpdl/warpdl/releases](https://github.com/warpdl/warpdl/releases)
- Binaries installed via Scoop (verified checksums)
- Binaries you built yourself from source

### Disable SmartScreen (Not Recommended)

Only disable if you frequently encounter issues and understand the security implications:

1. Open **Windows Security**
2. Go to **App & browser control**
3. Click **Reputation-based protection settings**
4. Toggle off **Check apps and files**

**Warning**: This reduces your system's security. Re-enable after installing WarpDL.

## Common Issues

### Issue: "Daemon failed to start"

**Symptoms**: `warpdl` commands hang or fail to connect

**Solutions**:
1. Check if daemon is running:
   - `warpdl status`
   - Or in PowerShell: `tasklist /FI "IMAGENAME eq warpdl.exe"`
2. Check if port 3849 is blocked:
   ```powershell
   netstat -ano | findstr :3849
   ```
3. Check firewall rules (see [Manual Firewall Configuration](#manual-firewall-rule-configuration))
4. Try starting daemon manually: `warpdl daemon` (run in separate terminal)
5. Check logs (if available) in `%USERPROFILE%\.config\warpdl\`

### Issue: "Connection refused"

**Symptoms**: CLI shows connection errors

**Cause**: Firewall blocking localhost TCP connections

**Solution**: Add firewall rule (see [Windows Firewall](#windows-firewall) section)

### Issue: Antivirus quarantines warpdl.exe

**Symptoms**: Binary disappears or downloads fail with permission errors

**Solutions**:
1. Restore from quarantine in your antivirus interface
2. Add exclusions (see [Antivirus Configuration](#antivirus-configuration))
3. Download from official sources to avoid tampered binaries

### Issue: Downloads fail immediately

**Symptoms**: Downloads start but fail within seconds

**Possible causes**:
- Antivirus scanning downloaded files (performance impact)
- Firewall blocking outbound connections
- Antivirus blocking write access to download directory

**Solutions**:
1. Add download directory to antivirus exclusions
2. Check Windows Firewall outbound rules (usually not restricted by default)
3. Verify write permissions on download directory

### Issue: High CPU/memory usage triggers antivirus

**Symptoms**: Daemon process flagged as suspicious due to resource usage

**Cause**: Parallel downloading uses multiple connections and threads

**Solution**: 
- This is normal behavior for download managers
- Add process exclusions to reduce false positives
- Reduce parallel connections if needed (configuration option)

## Getting Help

If you encounter issues not covered in this guide:

1. **Check existing issues**: [github.com/warpdl/warpdl/issues](https://github.com/warpdl/warpdl/issues)
2. **Open a new issue**: Include:
   - Windows version (run `winver`)
   - Antivirus/firewall software
   - Error messages or logs
   - Steps to reproduce
3. **Community support**: Check GitHub Discussions

## Additional Resources

- [Main README](../README.md)
- [Contributing Guide](../CONTRIBUTING.md)
- [GitHub Issues](https://github.com/warpdl/warpdl/issues)
- [Official Releases](https://github.com/warpdl/warpdl/releases)
