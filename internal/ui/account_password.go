package ui

import (
	"errors"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tg-dlp_GUI/internal/tgauth"
	"tg-dlp_GUI/internal/tgdlp"
)

// showChangePasswordDialog asks for the passwords, then changes them off the
// UI thread. onDone(message) runs on the UI thread after a successful change.
func showChangePasswordDialog(w fyne.Window, it tgdlp.Account, onDone func(msg string)) {
	curEntry := widget.NewPasswordEntry()
	curEntry.SetPlaceHolder("Leave empty if it is still remembered")

	newEntry := widget.NewPasswordEntry()
	newEntry.SetPlaceHolder("Min 4 characters")
	newEntry.Validator = tgauth.ValidateSessionPassword

	new2Entry := widget.NewPasswordEntry()
	new2Entry.SetPlaceHolder("Repeat the new password")
	new2Entry.Validator = func(s string) error {
		if s != newEntry.Text {
			return errors.New("The passwords do not match.")
		}
		return nil
	}

	var labels []string
	for _, c := range tgauth.CacheChoices {
		labels = append(labels, c.Label)
	}
	cacheSel := widget.NewSelect(labels, nil)
	cacheSel.SetSelectedIndex(0)

	note := widget.NewLabel("This is the password of the session file on this PC, not your Telegram password.\nDo not change it while a download is running.")
	note.Wrapping = fyne.TextWrapWord
	busyLbl := widget.NewLabel("")

	var d *dialog.CustomDialog
	saveBtn := widget.NewButtonWithIcon("Change password", theme.ConfirmIcon(), nil)
	saveBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() { d.Hide() })

	busy := false // UI thread only
	valid := func() bool {
		return tgauth.ValidateSessionPassword(newEntry.Text) == nil && newEntry.Text == new2Entry.Text
	}
	refresh := func() {
		if valid() && !busy {
			saveBtn.Enable()
		} else {
			saveBtn.Disable()
		}
	}
	newEntry.OnChanged = func(string) {
		if new2Entry.Text != "" {
			_ = new2Entry.Validate()
		}
		refresh()
	}
	new2Entry.OnChanged = func(string) { refresh() }

	run := func() {
		if busy || !valid() {
			return
		}
		cur, np := curEntry.Text, newEntry.Text
		idx := cacheSel.SelectedIndex()
		if idx < 0 {
			idx = 0
		}
		seconds := tgauth.CacheChoices[idx].Seconds

		busy = true
		refresh()
		cancelBtn.Disable()
		busyLbl.SetText("Re-encrypting the session...")

		go func() {
			cfgDir, err := tgdlp.ConfigDir()
			if err == nil {
				err = tgauth.ChangeSessionPassword(cfgDir, it.Profile, cur, np, seconds)
			}
			fyne.Do(func() {
				d.Hide()
				if err != nil {
					dialog.ShowError(errors.New(strings.ReplaceAll(err.Error(), np, "***")), w)
					return
				}
				onDone("Session password changed for " + tgdlp.CleanCLIText(it.Label) + " [profile: " + it.Profile + "]")
			})
		}()
	}
	saveBtn.OnTapped = run
	new2Entry.OnSubmitted = func(string) { run() }

	form := widget.NewForm(
		widget.NewFormItem("Account", widget.NewLabel(tgdlp.CleanCLIText(it.Label)+"   [profile: "+it.Profile+"]")),
		widget.NewFormItem("Current password", curEntry),
		widget.NewFormItem("New password", newEntry),
		widget.NewFormItem("Confirm password", new2Entry),
		widget.NewFormItem("Remember password", cacheSel),
	)
	content := container.NewVBox(form, note, busyLbl)
	d = dialog.NewCustomWithoutButtons("Change session password", content, w)
	d.SetButtons([]fyne.CanvasObject{cancelBtn, saveBtn})
	refresh() // starts disabled
	d.Resize(fyne.NewSize(560, 400))
	d.Show()
}
