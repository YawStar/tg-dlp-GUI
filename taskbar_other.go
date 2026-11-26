//go:build !windows

package main

import "fyne.io/fyne/v2"

// Taskbar progress is Windows-only; these are no-ops elsewhere.
func setTaskbarIndeterminate(w fyne.Window)            {}
func setTaskbarProgress(w fyne.Window, progress float64) {}
func setTaskbarError(w fyne.Window)                    {}
func clearTaskbarProgress(w fyne.Window)               {}
