package ui

import (
	"fmt"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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

// powerCountdownSeconds is how long the Shutdown / Reboot dialog waits.
const powerCountdownSeconds = 30

// newAfterTaskSelect builds the "after the task" drop-down. It always starts
// on "Do nothing" and is deliberately not saved, so a forgotten Shutdown
// choice can never fire in a later session.
func newAfterTaskSelect() *widget.Select {
	s := widget.NewSelect([]string{afterNothing, afterExit, afterShutdown, afterSleep, afterReboot}, nil)
	s.SetSelected(afterNothing)
	return s
}

// runAfterTask performs the chosen action. quit closes the application.
// Must be called on the UI thread.
func runAfterTask(a fyne.App, w fyne.Window, choice string, quit func()) {
	switch choice {
	case afterExit:
		quit()
	case afterSleep:
		if err := platform.Power(platform.PowerSleep); err != nil {
			dialog.ShowError(err, w)
		}
	case afterShutdown:
		showPowerCountdown(a, w, "Shutdown", "Shutting down", platform.PowerShutdown)
	case afterReboot:
		showPowerCountdown(a, w, "Reboot", "Rebooting", platform.PowerReboot)
	}
}

// showPowerCountdown shows a live countdown. "Abort" cancels the action,
// "Right now" runs it immediately, and when the time is up it runs by itself.
func showPowerCountdown(a fyne.App, w fyne.Window, title, ing string, action platform.PowerAction) {
	msg := widget.NewLabel("")
	msg.Alignment = fyne.TextAlignCenter
	setRemaining := func(sec int) {
		msg.SetText(fmt.Sprintf("%s in %d seconds...", ing, sec))
	}
	setRemaining(powerCountdownSeconds)

	bar := widget.NewProgressBar()
	bar.Max = powerCountdownSeconds
	bar.SetValue(powerCountdownSeconds)
	bar.TextFormatter = func() string { return fmt.Sprintf("%.0fs", bar.Value) }

	var (
		d       dialog.Dialog
		decided atomic.Bool // the first of Abort / Right now / timeout wins
		stop    = make(chan struct{})
	)
	execute := func() {
		if err := platform.Power(action); err != nil {
			dialog.ShowError(err, w)
		}
	}

	abortBtn := widget.NewButton("Abort", func() {
		if decided.CompareAndSwap(false, true) {
			close(stop)
			d.Hide()
		}
	})
	nowBtn := widget.NewButton("Right now", func() {
		if decided.CompareAndSwap(false, true) {
			close(stop)
			d.Hide()
			execute()
		}
	})
	nowBtn.Importance = widget.HighImportance

	d = dialog.NewCustomWithoutButtons(title, container.NewVBox(
		msg,
		bar,
		container.NewGridWithColumns(2, abortBtn, nowBtn),
	), w)
	d.Resize(fyne.NewSize(420, 170))
	d.Show()
	w.RequestFocus()
	// the window may be minimized, so also tell the user from outside
	a.SendNotification(fyne.NewNotification("tg-dlp GUI", fmt.Sprintf("%s in %d seconds.", ing, powerCountdownSeconds)))

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for remaining := powerCountdownSeconds; ; {
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
				fyne.Do(func() {
					setRemaining(r)
					bar.SetValue(float64(r))
				})
			}
		}
	}()
}
