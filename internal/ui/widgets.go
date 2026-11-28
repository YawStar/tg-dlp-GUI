package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func makeClearableEntry(entry *widget.Entry) fyne.CanvasObject {
	var clearBtn *widget.Button
	clearBtn = widget.NewButton("×", func() {
		entry.SetText("")
		entry.FocusGained()
	})
	clearBtn.Importance = widget.LowImportance

	updateClearVisibility := func(s string) {
		if strings.TrimSpace(s) == "" {
			clearBtn.Hide()
		} else {
			clearBtn.Show()
		}
	}

	entry.OnChanged = func(s string) {
		updateClearVisibility(s)
	}
	updateClearVisibility(entry.Text)

	rightOverlay := container.NewBorder(nil, nil, nil, clearBtn, layout.NewSpacer())
	return container.NewStack(entry, rightOverlay)
}

type compactVBoxLayout struct {
	gap float32
}

func (c *compactVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	visibleCount := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		h += m.Height
		visibleCount++
	}
	if visibleCount > 1 {
		h += c.gap * float32(visibleCount-1)
	}
	return fyne.NewSize(w, h)
}

func (c *compactVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	first := true
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		if !first {
			y += c.gap
		}
		first = false
		m := o.MinSize()
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, m.Height))
		y += m.Height
	}
}

func newCompactVBox(gap float32, objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(&compactVBoxLayout{gap: gap}, objects...)
}

type hoverBox struct {
	widget.BaseWidget
	btn      *widget.Button
	onTap    func()
	onChange func(hovered bool)
	hovered  bool
}

func (h *hoverBox) CreateRenderer() fyne.WidgetRenderer {
	rect := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(rect)
}

func (h *hoverBox) Cursor() desktop.Cursor {
	if h.hovered && (h.btn == nil || !h.btn.Disabled()) {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (h *hoverBox) MouseIn(_ *desktop.MouseEvent) {
	h.hovered = true
	if h.onChange != nil {
		h.onChange(true)
	}
}

func (h *hoverBox) MouseMoved(_ *desktop.MouseEvent) {
	if !h.hovered {
		h.hovered = true
		if h.onChange != nil {
			h.onChange(true)
		}
	}
}

func (h *hoverBox) MouseOut() {
	h.hovered = false
	if h.onChange != nil {
		h.onChange(false)
	}
}

func (h *hoverBox) Tapped(ev *fyne.PointEvent) {
	if h.btn != nil && !h.btn.Disabled() {
		h.btn.Tapped(ev)
		return
	}
	if h.onTap != nil {
		h.onTap()
	}
}

func newHoverBox(btn *widget.Button, onChange func(hovered bool)) *hoverBox {
	h := &hoverBox{
		btn:      btn,
		onChange: onChange,
	}
	h.ExtendBaseWidget(h)
	return h
}
