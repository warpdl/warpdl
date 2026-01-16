package nativehost

// OfficialChromeExtensionID is the official Chrome extension ID for WarpDL.
// This will be populated once the browser extension is published to the Chrome Web Store.
const OfficialChromeExtensionID = ""

// OfficialFirefoxExtensionID is the official Firefox extension ID for WarpDL.
// This will be populated once the browser extension is published to Firefox Add-ons.
const OfficialFirefoxExtensionID = ""

// HasOfficialExtensions returns true if at least one official extension ID is configured.
// Package manager hooks use this to determine if native host installation should proceed.
func HasOfficialExtensions() bool {
    return OfficialChromeExtensionID != "" || OfficialFirefoxExtensionID != ""
}
