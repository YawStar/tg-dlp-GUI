package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	tdaccounts "tg-dlp/core/accounts"
)

// ============================================================
// Add account (GUI version of the CLI "+ Add account" flow)
//
//   1. form: profile name, session password (min 4), remember for ...
//   2. qrlogin.go runs the QR login natively with the tg-dlp packages
//      (no tg-dlp process): the session file, password cache and .meta are
//      created exactly like "tg-dlp login --qr --profile <n>" does, and the
//      new account becomes the current one (like "tg-dlp switch <n>")
//   3. the login link is drawn as a QR code; the Telegram 2-step password
//      is asked inside the dialog when the account needs it
// ============================================================

const minSessionPassword = 4

type cacheChoice struct {
	Label   string
	Seconds int // 0 = until the session file is deleted, qrCacheNever = do not remember
}

// "Remember the session password for ..." (same options as "tg-dlp config-cache").
var cacheChoices = []cacheChoice{
	{"30 minutes (default)", 1800},
	{"1 hour", 3600},
	{"2 hours", 7200},
	{"3 hours", 10800},
	{"Once (do not remember)", qrCacheNever},
	{"Until session expires (forever)", 0},
}

// proxyFromArgs turns the global proxy flags the dialog receives
// ("--proxy <url>" / "--no-proxy") into the values the login needs.
func proxyFromArgs(args []string) (proxy string, noProxy bool) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--proxy":
			if i+1 < len(args) {
				proxy = args[i+1]
				i++
			}
		case "--no-proxy":
			noProxy = true
		}
	}
	return proxy, noProxy
}

func validateProfileName(name string, existing []accountItem) error {
	switch {
	case name == "":
		return errors.New("Please enter a profile name.")
	case strings.ContainsAny(name, " \t"):
		return errors.New("The profile name can't contain spaces.")
	case utf8.RuneCountInString(name) > 32:
		return errors.New("The profile name can be at most 32 characters.")
	case name == "default":
		return errors.New("'default' is reserved. Please choose another name.")
	case !profileNameRe.MatchString(name):
		return errors.New("The profile name can't contain special characters like < > ? | \\ * / : \". Use only letters, digits, '-' and '_'.")
	case profileExists(existing, name):
		return fmt.Errorf("An account named %q already exists.", name)
	}
	return nil
}

func validateSessionPassword(pw string) error {
	if utf8.RuneCountInString(pw) < minSessionPassword {
		return fmt.Errorf("The session password needs at least %d characters.", minSessionPassword)
	}
	return nil
}

// showAddAccountDialog asks for the details, then runs the whole flow.
// onDone(profile, added, message) is always called on the UI thread when the flow ends.
func showAddAccountDialog(w fyne.Window, existing []accountItem, proxyArgs func() []string, onDone func(profile string, added bool, msg string)) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("e.g. work")
	nameEntry.Validator = func(s string) error {
		return validateProfileName(strings.ToLower(strings.TrimSpace(s)), existing)
	}

	pwEntry := widget.NewPasswordEntry()
	pwEntry.SetPlaceHolder("Min 4 characters")
	pwEntry.Validator = validateSessionPassword

	pw2Entry := widget.NewPasswordEntry()
	pw2Entry.SetPlaceHolder("Repeat the password")
	pw2Entry.Validator = func(s string) error {
		if s != pwEntry.Text {
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

	note := widget.NewLabel("The session password only protects the session file on this PC.\nIt is NOT your Telegram password.")
	note.Wrapping = fyne.TextWrapWord

	// Continue is enabled ONLY when every field is valid (Fyne's own form dialog
	// ignores fields the user has not touched yet).
	var d *dialog.CustomDialog
	continueBtn := widget.NewButtonWithIcon("Continue", theme.ConfirmIcon(), nil)
	continueBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() { d.Hide() })

	allValid := func() bool {
		name := strings.ToLower(strings.TrimSpace(nameEntry.Text))
		return validateProfileName(name, existing) == nil &&
			validateSessionPassword(pwEntry.Text) == nil &&
			pwEntry.Text == pw2Entry.Text
	}
	refresh := func() {
		if allValid() {
			continueBtn.Enable()
		} else {
			continueBtn.Disable()
		}
	}
	nameEntry.OnChanged = func(string) { refresh() }
	pwEntry.OnChanged = func(string) {
		if pw2Entry.Text != "" {
			_ = pw2Entry.Validate() // show/clear the "do not match" message
		}
		refresh()
	}
	pw2Entry.OnChanged = func(string) { refresh() }

	proceed := func() {
		if !allValid() { // never trust the button state alone
			return
		}
		name := strings.ToLower(strings.TrimSpace(nameEntry.Text))
		idx := cacheSel.SelectedIndex()
		if idx < 0 {
			idx = 0
		}
		pw := pwEntry.Text
		choice := cacheChoices[idx]
		d.Hide()
		showAddProgressDialog(w, name, pw, choice, proxyArgs(), onDone)
	}
	continueBtn.OnTapped = proceed
	pw2Entry.OnSubmitted = func(string) { proceed() }

	form := widget.NewForm(
		widget.NewFormItem("Profile name", nameEntry),
		widget.NewFormItem("Session password", pwEntry),
		widget.NewFormItem("Confirm password", pw2Entry),
		widget.NewFormItem("Remember password", cacheSel),
		widget.NewFormItem("", note),
	)
	d = dialog.NewCustomWithoutButtons("Add account", form, w)
	d.SetButtons([]fyne.CanvasObject{cancelBtn, continueBtn})
	refresh() // starts disabled
	d.Resize(fyne.NewSize(540, 400))
	d.Show()
}

// showAddProgressDialog runs the QR login (qrlogin.go) and shows the QR code.
func showAddProgressDialog(w fyne.Window, name, pw string, choice cacheChoice, proxy []string, onDone func(profile string, added bool, msg string)) {
	titles := []string{
		"Prepare the session",
		"Scan the QR code with your phone",
		"Save the account",
	}
	stepLbls := make([]*widget.Label, len(titles))
	for i, t := range titles {
		stepLbls[i] = widget.NewLabel("[   ]  " + t)
	}
	setStep := func(i int, mark string) {
		fyne.Do(func() { stepLbls[i].SetText(mark + "  " + titles[i]) })
	}

	qrImg := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	qrImg.FillMode = canvas.ImageFillContain
	qrImg.ScaleMode = canvas.ImageScalePixels
	qrImg.SetMinSize(fyne.NewSize(280, 280))
	qrImg.Hide()

	hint := widget.NewLabel("Scan this QR code with Telegram on your phone:\nSettings > Devices > Link Desktop Device\n(the phone must be logged in to the account you want to use)")
	hint.Wrapping = fyne.TextWrapWord
	hint.Hide()

	curURL := "" // only touched on the UI thread
	linkEntry := widget.NewEntry()
	linkEntry.OnChanged = func(t string) { // selectable, not editable
		if t != curURL {
			linkEntry.SetText(curURL)
		}
	}
	copyBtn := widget.NewButtonWithIcon("Copy link", theme.ContentCopyIcon(), func() {
		if curURL != "" {
			w.Clipboard().SetContent(curURL)
		}
	})
	linkRow := container.NewBorder(nil, nil, nil, copyBtn, linkEntry)
	linkRow.Hide()

	statusLbl := widget.NewLabel("")
	statusLbl.Wrapping = fyne.TextWrapWord

	// Telegram two-step verification password (shown only when the account needs it).
	twoFALbl := widget.NewLabel("")
	twoFALbl.Wrapping = fyne.TextWrapWord
	twoFAEntry := widget.NewPasswordEntry()
	twoFAEntry.SetPlaceHolder("Telegram 2-step password")
	submitBtn := widget.NewButtonWithIcon("Send", theme.MailSendIcon(), nil)
	pwCh := make(chan string, 1)
	submit := func() {
		txt := twoFAEntry.Text
		if txt == "" {
			return
		}
		select {
		case pwCh <- txt:
			twoFAEntry.SetText("")
			twoFAEntry.Disable()
			submitBtn.Disable()
		default:
		}
	}
	submitBtn.OnTapped = submit
	twoFAEntry.OnSubmitted = func(string) { submit() }
	twoFABox := container.NewVBox(widget.NewSeparator(), twoFALbl, container.NewBorder(nil, nil, nil, submitBtn, twoFAEntry))
	twoFABox.Hide()

	var (
		finished  atomic.Bool
		cancelReq atomic.Bool
		curStep   atomic.Int32
	)

	content := container.NewVBox(
		stepLbls[0], stepLbls[1], stepLbls[2],
		widget.NewSeparator(),
		container.NewCenter(qrImg),
		hint,
		linkRow,
		statusLbl,
		twoFABox,
	)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(520, 520))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	d := dialog.NewCustom("Add account: "+name, "Cancel", scroll, w)
	d.SetOnClosed(func() {
		if finished.Load() {
			return
		}
		cancelReq.Store(true) // user pressed Cancel
		cancel()
	})
	d.Resize(fyne.NewSize(580, 620))
	d.Show()

	hooks := qrLoginHooks{
		ShowQR: func(u string) {
			q, err := qrcode.New(u, qrcode.Medium)
			if err != nil {
				return
			}
			img := q.Image(-6) // 6 px per module, sharp edges
			fyne.Do(func() {
				curURL = u
				linkEntry.SetText(u)
				qrImg.Image = img
				qrImg.Show()
				qrImg.Refresh()
				hint.Show()
				linkRow.Show()
			})
		},
		Stage: func(s qrStage) {
			switch s {
			case stageScan:
				if curStep.Swap(1) < 1 {
					setStep(0, "[ OK ]")
					setStep(1, "[...]")
				}
			case stageSaving:
				if curStep.Swap(2) < 2 {
					setStep(1, "[ OK ]")
					setStep(2, "[...]")
				}
			}
		},
		Status: func(msg string) {
			fyne.Do(func() { statusLbl.SetText(msg) })
		},
		AskPassword: func(ctx context.Context, retry bool) (string, error) {
			msg := "This account uses two-step verification.\nEnter your Telegram password (not the session password):"
			if retry {
				msg = "Wrong password. Try again:"
			}
			fyne.Do(func() {
				qrImg.Hide() // the QR code was already scanned
				hint.Hide()
				linkRow.Hide()
				twoFALbl.SetText(msg)
				twoFAEntry.Enable()
				submitBtn.Enable()
				twoFABox.Show()
				w.Canvas().Focus(twoFAEntry)
			})
			select {
			case p := <-pwCh:
				return p, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}

	// ---------- worker ----------
	go func() {
		defer cancel()
		setStep(0, "[...]")

		hidePW := func(s string) string { return strings.ReplaceAll(s, pw, "***") }
		end := func(added bool, msg string, showErr bool) {
			finished.Store(true)
			fyne.Do(func() {
				d.Hide()
				if showErr {
					dialog.ShowError(errors.New(msg), w)
				}
				onDone(name, added, msg)
			})
		}

		cfgDir, err := tgDlpConfigDir()
		if err != nil {
			setStep(0, "[FAIL]")
			end(false, "Could not add the account: "+err.Error(), true)
			return
		}
		proxyURL, noProxy := proxyFromArgs(proxy)

		err = runQRLogin(ctx, qrLoginParams{
			ConfigDir:    cfgDir,
			Profile:      name,
			Password:     pw,
			CacheSeconds: choice.Seconds,
			Proxy:        proxyURL,
			NoProxy:      noProxy,
		}, hooks)

		switch {
		case cancelReq.Load():
			end(false, "Cancelled - the account was not added.", false)
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			setStep(int(curStep.Load()), "[FAIL]")
			end(false, "The QR login timed out (5 minutes). Please try again.", true)
		case err != nil:
			setStep(int(curStep.Load()), "[FAIL]")
			end(false, "Could not add the account: "+hidePW(cleanCLIText(friendlyLoginError(err))), true)
		default:
			setStep(2, "[ OK ]")
			msg := "Account added: [profile: " + name + "]"
			if a, ok := tdaccounts.Get(cfgDir, name); ok {
				msg = fmt.Sprintf("Switched to %s [profile: %s]", cleanCLIText(a.Label()), name)
			}
			end(true, msg, false)
		}
	}()
}
