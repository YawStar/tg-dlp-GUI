package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tg-dlp_GUI/internal/platform"
)

// What to do when a task finished successfully.
const (
	afterNothing  = "Do nothing"
	afterExit     = "Exit App"
	afterShutdown = "Shutdown"
	afterSleep    = "Sleep"
	afterReboot   = "Reboot"
)

// How long the countdown dialog waits before the action runs by itself.
// Shutdown and Reboot can lose unsaved work, so they get more time than Sleep.
const (
	powerCountdownSeconds = 30
	sleepCountdownSeconds = 10
)

// newAfterTaskSelect builds the "after the task" drop-down and selects the
// saved choice (an unknown or empty value falls back to "Do nothing").
func newAfterTaskSelect(saved string) *widget.Select {
	options := []string{afterNothing, afterExit, afterShutdown, afterSleep, afterReboot}
	s := widget.NewSelect(options, nil)
	s.SetSelected(afterNothing)
	for _, o := range options {
		if o == saved {
			s.SetSelected(saved)
		}
	}
	return s
}

// runAfterTask performs the chosen action. quit closes the application.
// Must be called on the UI thread.
func runAfterTask(a fyne.App, w fyne.Window, choice string, quit func()) {
	switch choice {
	case afterExit:
		quit()
	case afterShutdown:
		showPowerCountdown(a, w, powerPlan{"Shutdown", "Shutting down", "Shutdown Now", powerCountdownSeconds, platform.PowerShutdown})
	case afterReboot:
		showPowerCountdown(a, w, powerPlan{"Reboot", "Rebooting", "Reboot Now", powerCountdownSeconds, platform.PowerReboot})
	case afterSleep:
		showPowerCountdown(a, w, powerPlan{"Sleep", "Going to sleep", "Sleep Now", sleepCountdownSeconds, platform.PowerSleep})
	}
}

// powerPlan describes one countdown dialog.
type powerPlan struct {
	title   string // dialog title
	ing     string // "Shutting down" -> "Shutting down in 30 seconds..."
	nowText string // text of the button that skips the countdown
	seconds int
	action  platform.PowerAction
}

// showPowerCountdown shows a live circular countdown. "Abort" cancels the
// action, the "... Now" button runs it immediately, and when the time is up
// it runs by itself.
func showPowerCountdown(a fyne.App, w fyne.Window, p powerPlan) {
	var left atomic.Int32 // seconds remaining, read by the ring painter
	left.Store(int32(p.seconds))

	msg := widget.NewLabel("")
	msg.Alignment = fyne.TextAlignCenter

	number := canvas.NewText("", theme.Color(theme.ColorNameForeground))
	number.TextSize = 40
	number.TextStyle = fyne.TextStyle{Bold: true}
	number.Alignment = fyne.TextAlignCenter

	ring := canvas.NewRaster(func(pw, ph int) image.Image {
		track := theme.Color(theme.ColorNameDisabled)
		fill := theme.Color(theme.ColorNamePrimary)
		return ringImage(pw, ph, float64(left.Load())/float64(p.seconds), track, fill)
	})
	ring.SetMinSize(fyne.NewSize(150, 150))

	show := func(sec int) {
		left.Store(int32(sec))
		msg.SetText(fmt.Sprintf("%s in %d seconds...", p.ing, sec))
		number.Text = fmt.Sprintf("%d", sec)
		number.Refresh()
		ring.Refresh()
	}
	show(p.seconds)

	var (
		d       dialog.Dialog
		decided atomic.Bool // the first of Abort / Now / timeout wins
		stop    = make(chan struct{})
	)
	execute := func() {
		if err := platform.Power(p.action); err != nil {
			dialog.ShowError(err, w)
		}
	}

	abortBtn := widget.NewButton("Abort", func() {
		if decided.CompareAndSwap(false, true) {
			close(stop)
			d.Hide()
		}
	})
	nowBtn := widget.NewButton(p.nowText, func() {
		if decided.CompareAndSwap(false, true) {
			close(stop)
			d.Hide()
			execute()
		}
	})
	nowBtn.Importance = widget.HighImportance

	d = dialog.NewCustomWithoutButtons(p.title, container.NewVBox(
		msg,
		container.NewCenter(container.NewStack(ring, container.NewCenter(number))),
		container.NewGridWithColumns(2, abortBtn, nowBtn),
	), w)
	d.Resize(fyne.NewSize(420, 330))
	d.Show()
	w.RequestFocus()
	// the window may be minimized, so also tell the user from outside
	a.SendNotification(fyne.NewNotification("tg-dlp GUI", fmt.Sprintf("%s in %d seconds.", p.ing, p.seconds)))

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for remaining := p.seconds; ; {
			select {
			case <-stop:
				return
			case <-t.C:
				remaining--
				if remaining <= 0 {
					if decided.CompareAndSwap(false, true) {
						fyne.Do(func() {
							d.Hide()
							execute()
						})
					}
					return
				}
				r := remaining
				fyne.Do(func() { show(r) })
			}
		}
	}()
}

// ringImage draws a ring that is filled clockwise from the top for the given
// fraction (0..1) and shows the rest in the track colour.
func ringImage(w, h int, frac float64, track, fill color.Color) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	outer := math.Min(cx, cy) - 1
	inner := outer * 0.84
	tr := color.NRGBAModel.Convert(track).(color.NRGBA)
	fl := color.NRGBAModel.Convert(fill).(color.NRGBA)
	limit := frac * 2 * math.Pi
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Hypot(dx, dy)
			cov := math.Min(d-inner+0.5, outer-d+0.5) // 1px soft edge
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			ang := math.Atan2(dx, -dy) // 0 at the top, clockwise
			if ang < 0 {
				ang += 2 * math.Pi
			}
			c := tr
			if ang <= limit {
				c = fl
			}
			c.A = uint8(float64(c.A) * cov)
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}
