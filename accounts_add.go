package main

import (
	"errors"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
)

// ============================================================
// Add account (GUI version of the CLI "+ Add account" flow)
//
//   1. form: profile name, session password (min 4), remember for ...
//   2. tg-dlp config-pw <password> --profile <name>
//   3. tg-dlp config-cache <seconds> --profile <name>
//   4. tg-dlp login --qr-url --profile <name>   -> the printed URL is drawn as a QR code
//   5. tg-dlp switch <name>
// ============================================================

const minSessionPassword = 4

type cacheChoice struct {
	Label   string
	Seconds int
}

// "Remember the session password for ..." -> value given to "tg-dlp config-cache".
// 0 = until the session file is deleted (never expires).
// If your tg-dlp expects the menu number (1-4 / 0) instead of seconds, change it HERE only.
var cacheChoices = []cacheChoice{
	{"30 minutes (default)", 1800},
	{"1 hour", 3600},
	{"2 hours", 7200},
	{"3 hours", 10800},
	{"Until session expires (forever)", 0},
}

var (
	oscRe      = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	loginURLRe = regexp.MustCompile(`tg://[^\s"'<>\x1b\x00-\x1f]+`)
)

// cleanPTY strips terminal control sequences from raw pty output.
func cleanPTY(s string) string {
	return stripANSI(oscRe.ReplaceAllString(s, ""))
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

// showAddProgressDialog runs the commands and shows the login QR code.
func showAddProgressDialog(w fyne.Window, name, pw string, choice cacheChoice, proxy []string, onDone func(profile string, added bool, msg string)) {
	titles := []string{
		"Set session password  (config-pw)",
		"Set password cache  (config-cache)",
		"Log in with the QR code  (login --qr-url)",
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

	curURL := ""
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

	outputLbl := widget.NewLabel("")
	outputLbl.Wrapping = fyne.TextWrapWord

	var (
		finished  atomic.Bool
		cancelReq atomic.Bool
		ptyMu     sync.Mutex
		curPTY    ptySession
	)

	inputEntry := widget.NewPasswordEntry()
	inputEntry.SetPlaceHolder("Type here and press Send")
	sendBtn := widget.NewButtonWithIcon("Send", theme.MailSendIcon(), nil)
	sendBtn.OnTapped = func() {
		txt := inputEntry.Text
		inputEntry.SetText("")
		ptyMu.Lock()
		p := curPTY
		ptyMu.Unlock()
		if p != nil {
			_, _ = p.Write([]byte(txt + "\r"))
		}
	}
	inputEntry.OnSubmitted = func(string) { sendBtn.OnTapped() }
	inputEntry.Disable()
	sendBtn.Disable()

	content := container.NewVBox(
		stepLbls[0], stepLbls[1], stepLbls[2],
		widget.NewSeparator(),
		container.NewCenter(qrImg),
		hint,
		linkRow,
		outputLbl,
		widget.NewSeparator(),
		widget.NewLabel("If tg-dlp asks for input (e.g. your Telegram 2-step password):"),
		container.NewBorder(nil, nil, nil, sendBtn, inputEntry),
	)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(520, 520))

	d := dialog.NewCustom("Add account: "+name, "Cancel", scroll, w)
	d.SetOnClosed(func() {
		if finished.Load() {
			return
		}
		cancelReq.Store(true) // user pressed Cancel
		ptyMu.Lock()
		p := curPTY
		ptyMu.Unlock()
		if p != nil {
			p.Kill()
		}
	})
	d.Resize(fyne.NewSize(580, 620))
	d.Show()

	// ---------- worker ----------
	go func() {
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
		failStep := func(i int, out string, err error) {
			setStep(i, "[FAIL]")
			msg := cleanCLIText(hidePW(out))
			if msg == "" {
				msg = hidePW(err.Error())
			}
			end(false, "Could not add the account: "+msg, true)
		}
		cancelled := func() bool {
			if cancelReq.Load() {
				end(false, "Cancelled - the account was not added.", false)
				return true
			}
			return false
		}

		// 1) config-pw
		setStep(0, "[...]")
		if out, err := runTgDlpHidden(30*time.Second, "config-pw", pw, "--profile", name); err != nil {
			failStep(0, out, err)
			return
		}
		setStep(0, "[ OK ]")
		if cancelled() {
			return
		}

		// 2) config-cache
		setStep(1, "[...]")
		if out, err := runTgDlpHidden(30*time.Second, "config-cache", strconv.Itoa(choice.Seconds), "--profile", name); err != nil {
			failStep(1, out, err)
			return
		}
		setStep(1, "[ OK ]")
		if cancelled() {
			return
		}

		// 3) login --qr-url  (hidden pty; the printed URL becomes a QR code)
		setStep(2, "[...]")
		exe, err := resolveTgDlpPath()
		if err != nil {
			failStep(2, "", err)
			return
		}
		args := append(append([]string{}, proxy...), "login", "--qr-url", "--profile", name)
		pty, err := startPTY(exe, args)
		if err != nil {
			failStep(2, "", err)
			return
		}
		ptyMu.Lock()
		curPTY = pty
		ptyMu.Unlock()
		if cancelReq.Load() { // Cancel was pressed while the pty was starting
			pty.Kill()
		}
		fyne.Do(func() {
			inputEntry.Enable()
			sendBtn.Enable()
		})

		var (
			bufMu sync.Mutex
			buf   strings.Builder
		)
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			tmp := make([]byte, 4096)
			for {
				n, rerr := pty.Read(tmp)
				if n > 0 {
					bufMu.Lock()
					buf.Write(tmp[:n])
					bufMu.Unlock()
				}
				if rerr != nil {
					return
				}
			}
		}()
		snapshot := func() string {
			bufMu.Lock()
			defer bufMu.Unlock()
			return cleanPTY(buf.String())
		}

		exited := make(chan int, 1)
		go func() {
			code, _ := pty.Wait()
			exited <- code
		}()

		lastURL, lastTail := "", ""
		exitCode, timedOut := -1, false
		tick := time.NewTicker(250 * time.Millisecond)
		timeout := time.After(5 * time.Minute)

	wait:
		for {
			select {
			case exitCode = <-exited:
				break wait
			case <-timeout:
				timedOut = true
				pty.Kill()
				select {
				case exitCode = <-exited:
				case <-time.After(3 * time.Second):
				}
				break wait
			case <-tick.C:
				snap := snapshot()

				// the newest URL wins (tg-dlp refreshes the QR code automatically)
				if all := loginURLRe.FindAllString(snap, -1); len(all) > 0 {
					if u := all[len(all)-1]; u != lastURL {
						lastURL = u
						if q, qerr := qrcode.New(u, qrcode.Medium); qerr == nil {
							img := q.Image(-6) // 6 px per module, sharp edges
							url := u
							fyne.Do(func() {
								curURL = url
								linkEntry.SetText(url)
								qrImg.Image = img
								qrImg.Show()
								qrImg.Refresh()
								hint.Show()
								linkRow.Show()
							})
						}
					}
				}

				// last few lines tg-dlp printed (without the URL itself)
				var keep []string
				for _, ln := range strings.Split(snap, "\n") {
					if strings.TrimSpace(ln) != "" && !strings.Contains(ln, "tg://") {
						keep = append(keep, ln)
					}
				}
				tail := cleanCLIText(strings.Join(keep, "\n"))
				if tail != lastTail {
					lastTail = tail
					fyne.Do(func() { outputLbl.SetText(tail) })
				}
			}
		}
		tick.Stop()
		_ = pty.Close()
		select {
		case <-readDone:
		case <-time.After(2 * time.Second):
		}
		final := hidePW(cleanCLIText(snapshot()))

		switch {
		case cancelReq.Load():
			end(false, "Cancelled - the account was not added.", false)
			return
		case timedOut:
			setStep(2, "[FAIL]")
			end(false, "The QR login timed out (5 minutes). Please try again.", true)
			return
		case exitCode != 0:
			setStep(2, "[FAIL]")
			if final == "" {
				final = "tg-dlp exited with code " + strconv.Itoa(exitCode)
			}
			end(false, "Login failed: "+final, true)
			return
		}

		// 4) make sure the account exists, then make it the current one
		after, lerr := listAccounts()
		if lerr != nil || !profileExists(after, name) {
			setStep(2, "[FAIL]")
			end(false, "Login finished, but the new account was not found in the list.", true)
			return
		}
		setStep(2, "[ OK ]")
		msg := "Account added: [profile: " + name + "]"
		if out, serr := runTgDlpHidden(20*time.Second, "switch", name); serr == nil {
			if t := cleanCLIText(out); t != "" {
				msg = t
			}
		}
		end(true, msg, false)
	}()
}
