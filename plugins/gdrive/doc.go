// Package gdrive contains the warpdl Google Drive extension. The
// runnable plugin code is in main.js and its metadata in manifest.json;
// this Go package exists only so the colocated test suite can exercise
// the JavaScript module in an isolated goja runtime.
//
// To install the plugin:
//
//	warp ext install plugins/gdrive
//
// After installation, paste any Google Drive share link (file, Docs,
// Sheets, Slides) as the URL argument to `warp download` and warpdl
// will resolve it to a direct download URL automatically.
package gdrive
