package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
)

// ============================================================
// Download: filter (-i) and filename template (-t)
// ============================================================

const invalidFilenameChars = `\/:*?"<>|`

// validateFilenameTemplate checks a template for "tg-dlp dl -t". Any
// placeholder is allowed as-is, and templates without placeholders are
// allowed; it only rejects characters that cannot be used in filenames.
func validateFilenameTemplate(t string) error {
	if strings.ContainsAny(t, invalidFilenameChars) {
		return fmt.Errorf("Template contains invalid filename characters (%s).", invalidFilenameChars)
	}
	return nil
}

// splitFilterTemplate returns the filter (-i) and template (-t) to use.
// Older versions of the GUI kept both in one field and told them apart by a
// "{": a saved value like "{id}_{name}" in the filter field is treated as the
// template, so old settings still work.
func splitFilterTemplate(filter, tmpl string) (string, string) {
	if tmpl == "" && strings.Contains(filter, "{") {
		return "", filter
	}
	return filter, tmpl
}

// equalHBoxLayout puts the VISIBLE children side by side with the same width
// (a hidden child gives its space to the others, which GridWithColumns does not).
type equalHBoxLayout struct {
	gap float32
}

func visibleObjects(objs []fyne.CanvasObject) []fyne.CanvasObject {
	var vis []fyne.CanvasObject
	for _, o := range objs {
		if o.Visible() {
			vis = append(vis, o)
		}
	}
	return vis
}

func (l *equalHBoxLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	vis := visibleObjects(objs)
	var w, h float32
	for _, o := range vis {
		ms := o.MinSize()
		if ms.Width > w {
			w = ms.Width
		}
		if ms.Height > h {
			h = ms.Height
		}
	}
	if n := len(vis); n > 0 {
		w = w*float32(n) + l.gap*float32(n-1)
	}
	return fyne.NewSize(w, h)
}

func (l *equalHBoxLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	vis := visibleObjects(objs)
	n := len(vis)
	if n == 0 {
		return
	}
	w := (size.Width - l.gap*float32(n-1)) / float32(n)
	x := float32(0)
	for _, o := range vis {
		o.Resize(fyne.NewSize(w, size.Height))
		o.Move(fyne.NewPos(x, 0))
		x += w + l.gap
	}
}
