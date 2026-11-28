package ui

import (
	"fyne.io/fyne/v2"
)

// equalHBoxLayout puts the VISIBLE children side by side with the same width
// (a hidden child gives its space to the others, which GridWithColumns does not).
type equalHBoxLayout struct {
	gap float32
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

func visibleObjects(objs []fyne.CanvasObject) []fyne.CanvasObject {
	var vis []fyne.CanvasObject
	for _, o := range objs {
		if o.Visible() {
			vis = append(vis, o)
		}
	}
	return vis
}
