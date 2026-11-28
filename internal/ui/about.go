// Package ui is the Fyne based user interface of tg-dlp GUI.
package ui

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tg-dlp_GUI/assets"
	"tg-dlp_GUI/internal/persist"
)

const (
	AppName        = "tg-dlp GUI"
	AppVersion     = "1.0.1.4"
	AppReleaseDate = "30.Nov.2026"
	AppDeveloper   = "YawHackka (YawStar)"
	AppRepoURL     = "https://github.com/YawStar/tg-dlp-gui"
	AppUpstreamURL = "https://github.com/YawStar/tg-dlp"
	AppDescription = "Full-featured GUI wrapper for tg-dlp.\nDownload, upload, export, backup, restore, info, delete, purge, accounts & session tools."
)

func newLink(text, raw string) fyne.CanvasObject {
	u, err := url.Parse(raw)
	if err != nil {
		return widget.NewLabel(raw)
	}
	return widget.NewHyperlink(text, u)
}

// aboutText is what "Copy info" puts on the clipboard (handy for bug reports).
func aboutText() string {
	return fmt.Sprintf("%s %s (%s)\nGo %s, %s/%s\n%s",
		AppName, AppVersion, AppReleaseDate,
		strings.TrimPrefix(runtime.Version(), "go"), runtime.GOOS, runtime.GOARCH,
		AppRepoURL)
}

func showAboutDialog(a fyne.App, w fyne.Window) {
	center := func(s string, style widget.RichTextStyle) *widget.RichText {
		style.Alignment = fyne.TextAlignCenter
		return widget.NewRichText(&widget.TextSegment{Text: s, Style: style})
	}

	var items []fyne.CanvasObject

	if len(assets.AppIcon) > 0 {
		img := canvas.NewImageFromResource(fyne.NewStaticResource("about-icon.png", assets.AppIcon))
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(72, 72))
		items = append(items, container.NewCenter(img))
	}

	items = append(items,
		center(AppName, widget.RichTextStyle{
			SizeName:  theme.SizeNameHeadingText,
			TextStyle: fyne.TextStyle{Bold: true},
		}),
		center("Version "+AppVersion, widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder}),
		widget.NewSeparator(),
	)

	desc := widget.NewLabel(AppDescription)
	desc.Alignment = fyne.TextAlignCenter
	desc.Wrapping = fyne.TextWrapWord

	info := widget.NewForm(
		widget.NewFormItem("Version", widget.NewLabel(AppVersion)),
		widget.NewFormItem("Released", widget.NewLabel(AppReleaseDate)),
		widget.NewFormItem("Developer", widget.NewLabel(AppDeveloper)),
		widget.NewFormItem("Repository", newLink(strings.TrimPrefix(AppRepoURL, "https://"), AppRepoURL)),
		widget.NewFormItem("Powered by", newLink("tg-dlp", AppUpstreamURL)),
		widget.NewFormItem("Built with", widget.NewLabel(fmt.Sprintf("Go %s, Fyne (%s/%s)",
			strings.TrimPrefix(runtime.Version(), "go"), runtime.GOOS, runtime.GOARCH))),
	)

	copyBtn := widget.NewButtonWithIcon("Copy info", theme.ContentCopyIcon(), func() {
		w.Clipboard().SetContent(aboutText())
	})
	folderBtn := widget.NewButtonWithIcon("Data folder", theme.FolderOpenIcon(), func() {
		if err := openLocalPath(a, persist.AppDataDir()); err != nil {
			dialog.ShowError(err, w)
		}
	})

	items = append(items,
		desc,
		widget.NewSeparator(),
		info,
		container.NewCenter(container.NewHBox(copyBtn, folderBtn)),
	)

	d := dialog.NewCustom("About "+AppName, "Close", container.NewVBox(items...), w)
	d.Resize(fyne.NewSize(500, 520))
	d.Show()
}
