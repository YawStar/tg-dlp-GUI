// Package assets embeds the static resources of the application.
package assets

import _ "embed"

// AppIcon is the window / application icon (PNG).
//
//go:embed app-icon.png
var AppIcon []byte
