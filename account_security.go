package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	tdaccounts "tg-dlp/core/accounts"
	tdsession "tg-dlp/core/session"
)

// ============================================================
// Change the session password / "remember password" time of one
// account. This replaces the old "config-pw" and "config-cache"
// buttons of the Settings dialog.
//
// It works on the session file directly (tg-dlp/core/session), like
// "tg-dlp config-session-password" does, so it does not need a
// terminal to ask for the current password.
// ============================================================

// accountProfileKey converts the profile name shown in the list ("default")
// into the key tg-dlp's accounts package uses ("" = the default slot).
func accountProfileKey(profile string) string {
	if profile == "default" {
		return ""
	}
	return profile
}

// applySessionCache sets how long the session password is remembered
// (same meaning as in qrlogin.go: 0 = until the session file is deleted,
// qrCacheNever = ask every time, > 0 = seconds).
func applySessionCache(sessionPath, password string, seconds int) {
	switch {
	case seconds == qrCacheNever:
		tdsession.ClearPasswordCache(sessionPath)
	case seconds == 0:
		tdsession.SavePasswordToCache(sessionPath, password, 100*365*24*time.Hour) // 100 years
	default:
		tdsession.SavePasswordToCache(sessionPath, password, time.Duration(seconds)*time.Second)
	}
}

// changeSessionPassword re-encrypts the session of a profile with a new
// password. current may be empty when the old password is still remembered.
func changeSessionPassword(configDir, profile, current, newPass string, cacheSeconds int) error {
	if utf8.RuneCountInString(newPass) < minSessionPassword {
		return fmt.Errorf("The new password needs at least %d characters.", minSessionPassword)
	}
	sessionPath := tdaccounts.SessionPath(configDir, accountProfileKey(profile))
	if !qrFileExists(sessionPath) {
		return errors.New("This account no longer exists.")
	}
	ctx := context.Background()

	// 1. open the session with the current password
	var data []byte
	if current != "" {
		st := &tdsession.EncryptedSessionStorage{Path: sessionPath, Password: current}
		d, err := st.LoadSession(ctx)
		if err != nil {
			return errors.New("The current session password is wrong.")
		}
		data = d
	} else if cached, ok := tdsession.LoadPasswordFromCache(sessionPath); ok {
		st := &tdsession.EncryptedSessionStorage{Path: sessionPath, Password: cached}
		if d, err := st.LoadSession(ctx); err == nil {
			data = d
		}
	}
	if data == nil {
		return errors.New("Please enter the current session password (it is no longer remembered).")
	}

	// 2. store it again with the new password (temp file + rename)
	newStore := &tdsession.EncryptedSessionStorage{Path: sessionPath, Password: newPass}
	if err := newStore.StoreSession(ctx, data); err != nil {
		return fmt.Errorf("Could not save the session: %w", err)
	}
	// 3. read it back before telling the user it worked
	if _, err := newStore.LoadSession(ctx); err != nil {
		return errors.New("The session was saved, but reading it back with the new password failed. Try again.")
	}

	// 4. the cached copy holds the OLD password
	tdsession.ClearPasswordCache(sessionPath)
	tdsession.ClearPendingPassword(sessionPath)
	applySessionCache(sessionPath, newPass, cacheSeconds)
	return nil
}

// showChangePasswordDialog asks for the passwords, then changes them off the
// UI thread. onDone(message) runs on the UI thread after a successful change.
func showChangePasswordDialog(w fyne.Window, it accountItem, onDone func(msg string)) {
	curEntry := widget.NewPasswordEntry()
	curEntry.SetPlaceHolder("Leave empty if it is still remembered")

	newEntry := widget.NewPasswordEntry()
	newEntry.SetPlaceHolder("Min 4 characters")
	newEntry.Validator = validateSessionPassword

	new2Entry := widget.NewPasswordEntry()
	new2Entry.SetPlaceHolder("Repeat the new password")
	new2Entry.Validator = func(s string) error {
		if s != newEntry.Text {
			return errors.New("The passwords do not match.")
		}
		return nil
	}

	var labels []string
	for _, c := range cacheChoices {
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
		return validateSessionPassword(newEntry.Text) == nil && newEntry.Text == new2Entry.Text
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
		seconds := cacheChoices[idx].Seconds

		busy = true
		refresh()
		cancelBtn.Disable()
		busyLbl.SetText("Re-encrypting the session...")

		go func() {
			cfgDir, err := tgDlpConfigDir()
			if err == nil {
				err = changeSessionPassword(cfgDir, it.Profile, cur, np, seconds)
			}
			fyne.Do(func() {
				d.Hide()
				if err != nil {
					dialog.ShowError(errors.New(strings.ReplaceAll(err.Error(), np, "***")), w)
					return
				}
				onDone("Session password changed for " + cleanCLIText(it.Label) + " [profile: " + it.Profile + "]")
			})
		}()
	}
	saveBtn.OnTapped = run
	new2Entry.OnSubmitted = func(string) { run() }

	form := widget.NewForm(
		widget.NewFormItem("Account", widget.NewLabel(cleanCLIText(it.Label)+"   [profile: "+it.Profile+"]")),
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
