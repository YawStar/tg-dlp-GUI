package ui

import (
	"net/url"
	"runtime"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	tgDlpReleasesURL = "https://github.com/YawStar/tg-dlp/releases/latest"
	tgDlpInstallPS   = "irm https://raw.githubusercontent.com/YawStar/tg-dlp/main/install.ps1 | iex"
)

var notFoundDialogOpen atomic.Bool

// showTgDlpNotFoundDialog tells the user tg-dlp is missing and how to get it.
// Must be called on the UI thread (inside fyne.Do when called from a goroutine).
func showTgDlpNotFoundDialog(w fyne.Window) {
	// Several startup checks can fail at once; show only one dialog.
	if !notFoundDialogOpen.CompareAndSwap(false, true) {
		return
	}

	msg := widget.NewLabel("tg-dlp was not found in PATH or in the application folder.\nInstall it, then restart this app.")
	msg.Wrapping = fyne.TextWrapWord

	items := []fyne.CanvasObject{msg}

	if u, err := url.Parse(tgDlpReleasesURL); err == nil {
		items = append(items, widget.NewHyperlink("Download tg-dlp (GitHub Releases)", u))
	}

	if runtime.GOOS == "windows" {
		psLbl := widget.NewLabel("Or run this in PowerShell:")
		psEntry := widget.NewEntry()
		psEntry.SetText(tgDlpInstallPS)
		// keep it selectable/copyable but not editable
		psEntry.OnChanged = func(t string) {
			if t != tgDlpInstallPS {
				psEntry.SetText(tgDlpInstallPS)
			}
		}
		copyBtn := widget.NewButtonWithIcon("Copy command", theme.ContentCopyIcon(), func() {
			w.Clipboard().SetContent(tgDlpInstallPS)
		})
		items = append(items, psLbl, psEntry, copyBtn)
	}

	d := dialog.NewCustom("tg-dlp not found", "OK", container.NewVBox(items...), w)
	d.SetOnClosed(func() { notFoundDialogOpen.Store(false) })
	d.Resize(fyne.NewSize(600, 260))
	d.Show()
}
