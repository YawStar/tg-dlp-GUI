//go:build !windows

package platform

import (
	"fyne.io/fyne/v2"
)

// Taskbar progress is Windows-only; these are no-ops elsewhere.
func SetTaskbarIndeterminate(w fyne.Window) {}

func SetTaskbarProgress(w fyne.Window, progress float64) {}

func SetTaskbarError(w fyne.Window) {}

func ClearTaskbarProgress(w fyne.Window) {}
