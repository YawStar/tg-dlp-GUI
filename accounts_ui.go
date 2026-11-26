package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ============================================================
// Talks to the tg-dlp CLI:
//   list    tg-dlp accounts --list        (parsed)
//   switch  tg-dlp switch <profile>
//   rename  tg-dlp rename <profile> <new>
//   remove  drives the interactive "tg-dlp accounts" menu inside a hidden pty
//           ("-", <number>, "y"), so tg-dlp itself deletes the session
//   add     opens a terminal for "tg-dlp login --qr --profile <name>" (QR code)
// ============================================================

type accountItem struct {
	Index   int    // 1-based number as printed by "tg-dlp accounts"
	Label   string // "Name (@username)"
	Profile string // profile name ("default" for the unnamed one)
	Current bool
}

// "  3. ● Name (@user)   [profile: work]  (current)"
var accountLineRe = regexp.MustCompile(`^\s*(\d+)\.\s+(?:●\s+)?(.+?)\s+\[profile:\s*([^\]]*)\]\s*(\(current\))?\s*$`)

var profileNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func parseAccounts(out string) []accountItem {
	var items []accountItem
	for _, line := range strings.Split(stripANSI(out), "\n") {
		m := accountLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		items = append(items, accountItem{
			Index:   n,
			Label:   strings.TrimSpace(m[2]),
			Profile: strings.TrimSpace(m[3]),
			Current: m[4] != "",
		})
	}
	return items
}

// runTgDlpHidden runs a non-interactive tg-dlp command without any console window.
func runTgDlpHidden(timeout time.Duration, args ...string) (string, error) {
	exe, err := resolveTgDlpPath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = errors.New("tg-dlp did not answer in time")
	}
	return strings.TrimSpace(stripANSI(string(out))), err
}

func listAccounts() ([]accountItem, error) {
	out, err := runTgDlpHidden(20*time.Second, "accounts", "--list")
	items := parseAccounts(out)
	if err != nil && len(items) == 0 {
		msg := cleanCLIText(out)
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return items, nil
}

// cleanCLIText removes emoji (the UI font can't draw them), blank lines,
// and keeps the last few lines.
func cleanCLIText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0xFE0F || r == 0x200D ||
			(r >= 0x1F000 && r <= 0x1FFFF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) {
			continue
		}
		b.WriteRune(r)
	}
	var lines []string
	for _, ln := range strings.Split(b.String(), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return strings.Join(lines, "\n")
}

// removeAccountViaCLI answers the prompts of the interactive menu
// (menu "-" -> account number -> "y") inside a hidden pseudo terminal.
func removeAccountViaCLI(number int) (string, error) {
	exe, err := resolveTgDlpPath()
	if err != nil {
		return "", err
	}
	pty, err := startPTY(exe, []string{"accounts"})
	if err != nil {
		return "", err
	}

	var (
		mu  sync.Mutex
		buf strings.Builder
	)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		tmp := make([]byte, 4096)
		for {
			n, err := pty.Read(tmp)
			if n > 0 {
				mu.Lock()
				buf.Write(tmp[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return stripANSI(buf.String())
	}
	waitFor := func(s string, d time.Duration) bool {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if strings.Contains(snapshot(), s) {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	send := func(s string) { _, _ = pty.Write([]byte(s + "\r")) }
	abort := func(msg string) (string, error) {
		pty.Kill()
		_ = pty.Close()
		return cleanCLIText(snapshot()), errors.New(msg)
	}

	if !waitFor("Select an account number", 15*time.Second) {
		return abort("tg-dlp did not show the account menu")
	}
	send("-")
	if !waitFor("Remove which account number", 10*time.Second) {
		return abort("tg-dlp did not ask which account to remove")
	}
	send(strconv.Itoa(number))
	if !waitFor("Continue?", 10*time.Second) {
		return abort("tg-dlp did not ask for confirmation")
	}
	send("y")

	done := make(chan struct{})
	go func() {
		_, _ = pty.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		pty.Kill()
	}
	_ = pty.Close()
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
	}
	return cleanCLIText(snapshot()), nil
}

func profileExists(items []accountItem, name string) bool {
	for _, it := range items {
		if it.Profile == name {
			return true
		}
	}
	return false
}

// showAccountsDialog is the GUI version of the "tg-dlp accounts" menu.
//   proxyArgs: extra global flags (e.g. --proxy) used for the QR login
//   onChanged: called after the active account / account list changed
func showAccountsDialog(w fyne.Window, proxyArgs func() []string, onChanged func()) {
	var (
		items    []accountItem
		selected = -1
		busy     = true
		list     *widget.List
	)

	statusLbl := widget.NewLabel("Loading accounts...")
	statusLbl.Wrapping = fyne.TextWrapWord

	switchBtn := widget.NewButtonWithIcon("Switch to selected", theme.ConfirmIcon(), nil)
	switchBtn.Importance = widget.HighImportance
	addBtn := widget.NewButtonWithIcon("  Add account (QR login)", theme.ContentAddIcon(), nil)
	renameBtn := widget.NewButtonWithIcon("Rename profile", theme.DocumentCreateIcon(), nil)
	removeBtn := widget.NewButtonWithIcon("-  Remove", theme.ContentRemoveIcon(), nil)
	removeBtn.Importance = widget.DangerImportance
	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), nil)

	current := func() (accountItem, bool) {
		if selected < 0 || selected >= len(items) {
			return accountItem{}, false
		}
		return items[selected], true
	}

	setEnabled := func(b *widget.Button, on bool) {
		if on {
			b.Enable()
		} else {
			b.Disable()
		}
	}
	updateButtons := func() {
		it, ok := current()
		setEnabled(switchBtn, !busy && ok && !it.Current)
		setEnabled(renameBtn, !busy && ok)
		setEnabled(removeBtn, !busy && ok)
		setEnabled(addBtn, !busy)
		setEnabled(refreshBtn, !busy)
	}

	// reload fetches the list again; reselect = profile to highlight ("" = current account)
	var reload func(reselect, message string)
	reload = func(reselect, message string) {
		busy = true
		if message != "" {
			statusLbl.SetText(message)
		}
		updateButtons()
		go func() {
			got, err := listAccounts()
			fyne.Do(func() {
				busy = false
				if err != nil {
					items = nil
					statusLbl.SetText("Could not read the account list:\n" + err.Error())
				} else {
					items = got
					if message == "" {
						if len(items) == 0 {
							statusLbl.SetText("No saved accounts yet. Press \"Add account\" to log in.")
						} else {
							statusLbl.SetText(fmt.Sprintf("%d account(s). Select one, then press a button.", len(items)))
						}
					}
				}
				selected = -1
				list.UnselectAll()
				list.Refresh()
				for i, it := range items {
					if (reselect != "" && it.Profile == reselect) || (reselect == "" && it.Current) {
						list.Select(i)
						break
					}
				}
				updateButtons()
			})
		}()
	}

	// run executes a slow job off the UI thread, then reloads the list
	run := func(startMsg, reselect string, changed bool, job func() (string, error)) {
		busy = true
		statusLbl.SetText(startMsg)
		updateButtons()
		go func() {
			out, err := job()
			fyne.Do(func() {
				msg := cleanCLIText(out)
				if err != nil {
					if msg != "" {
						msg = err.Error() + "\n" + msg
					} else {
						msg = err.Error()
					}
					msg = "Error: " + msg
				}
				reload(reselect, msg)
				if changed && onChanged != nil {
					onChanged()
				}
			})
		}()
	}

	list = widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("template account name (@username)   [profile: name]   (current)")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(items) {
				return
			}
			it := items[id]
			txt := fmt.Sprintf("%d.  %s   [profile: %s]", it.Index, it.Label, it.Profile)
			if it.Current {
				txt += "   (current)"
			}
			l := o.(*widget.Label)
			l.TextStyle = fyne.TextStyle{Bold: it.Current}
			l.SetText(txt)
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		selected = id
		updateButtons()
	}
	list.OnUnselected = func(id widget.ListItemID) {
		if selected == id {
			selected = -1
		}
		updateButtons()
	}

	// ---- Switch ----
	switchBtn.OnTapped = func() {
		it, ok := current()
		if !ok {
			return
		}
		run("Switching...", it.Profile, true, func() (string, error) {
			return runTgDlpHidden(20*time.Second, "switch", it.Profile)
		})
	}

	// ---- Rename ----
	renameBtn.OnTapped = func() {
		it, ok := current()
		if !ok {
			return
		}
		entry := widget.NewEntry()
		entry.SetPlaceHolder("e.g. personal")
		d := dialog.NewForm(
			"Rename profile",
			"Rename", "Cancel",
			[]*widget.FormItem{
				widget.NewFormItem("Account", widget.NewLabel(it.Label)),
				widget.NewFormItem("Current name", widget.NewLabel(it.Profile)),
				widget.NewFormItem("New name", entry),
			},
			func(ok bool) {
				if !ok {
					return
				}
				newName := strings.ToLower(strings.TrimSpace(entry.Text))
				if !profileNameRe.MatchString(newName) {
					dialog.ShowError(errors.New("Use letters, digits, '-' and '_' (max 32 characters)."), w)
					return
				}
				run("Renaming...", newName, true, func() (string, error) {
					return runTgDlpHidden(20*time.Second, "rename", it.Profile, newName)
				})
			}, w)
		d.Show()
	}

	// ---- Remove ----
	removeBtn.OnTapped = func() {
		it, ok := current()
		if !ok {
			return
		}
		dialog.ShowConfirm(
			"Remove account",
			fmt.Sprintf("Remove %s [profile: %s]?\n\nThis deletes the local session on this PC.\n(Your Telegram account itself is not deleted.)", it.Label, it.Profile),
			func(yes bool) {
				if !yes {
					return
				}
				run("Removing...", "", true, func() (string, error) {
					// the number can change, so look it up again right before removing
					fresh, err := listAccounts()
					if err != nil {
						return "", err
					}
					num := 0
					for _, f := range fresh {
						if f.Profile == it.Profile {
							num = f.Index
						}
					}
					if num == 0 {
						return "", errors.New("this account is no longer in the list")
					}
					out, err := removeAccountViaCLI(num)
					if err != nil {
						return out, err
					}
					after, lerr := listAccounts()
					if lerr == nil && profileExists(after, it.Profile) {
						return out, errors.New("the account is still in the list")
					}
					return "Removed " + it.Label + " [profile: " + it.Profile + "]", nil
				})
			}, w)
	}

	// ---- Add (QR login in a terminal window) ----
	addBtn.OnTapped = func() {
		first := len(items) == 0
		entry := widget.NewEntry()
		if first {
			entry.SetPlaceHolder("Profile name (optional, empty = default)")
		} else {
			entry.SetPlaceHolder("Profile name, e.g. work")
		}
		hint := widget.NewLabel("A terminal window opens with a QR code.\nIn Telegram: Settings > Devices > Link Desktop Device, then scan it.\nWhen the login is finished, close the terminal window.")
		hint.Wrapping = fyne.TextWrapWord

		d := dialog.NewForm(
			"Add account",
			"Open QR login", "Cancel",
			[]*widget.FormItem{
				widget.NewFormItem("Name", entry),
				widget.NewFormItem("", hint),
			},
			func(ok bool) {
				if !ok {
					return
				}
				name := strings.ToLower(strings.TrimSpace(entry.Text))
				switch {
				case name == "" && !first:
					dialog.ShowError(errors.New("Please enter a profile name."), w)
					return
				case name != "" && (name == "default" || !profileNameRe.MatchString(name)):
					dialog.ShowError(errors.New("Invalid name: use letters, digits, '-' and '_' (max 32 characters). 'default' is reserved."), w)
					return
				case name != "" && profileExists(items, name):
					dialog.ShowError(fmt.Errorf("An account named %q already exists.", name), w)
					return
				}

				args := append([]string{}, proxyArgs()...)
				args = append(args, "login", "--qr")
				if name != "" {
					args = append(args, "--profile", name)
				}

				busy = true
				statusLbl.SetText("Waiting for the QR login... close the terminal window when it is finished.")
				updateButtons()

				runInteractiveInTerminal(w, args, func() {
					go func() {
						after, err := listAccounts()
						added := err == nil && ((name == "" && len(after) > len(items)) || (name != "" && profileExists(after, name)))
						switched := ""
						if added && name != "" {
							out, _ := runTgDlpHidden(20*time.Second, "switch", name)
							switched = cleanCLIText(out)
						}
						fyne.Do(func() {
							msg := "Account was not added."
							if added {
								msg = "Account added."
								if switched != "" {
									msg = switched
								}
							}
							reload(name, msg)
							if added && onChanged != nil {
								onChanged()
							}
						})
					}()
				})
			}, w)
		d.Show()
	}

	refreshBtn.OnTapped = func() {
		it, ok := current()
		keep := ""
		if ok {
			keep = it.Profile
		}
		reload(keep, "")
	}

	buttons := container.NewVBox(
		container.NewGridWithColumns(3, switchBtn, renameBtn, removeBtn),
		container.NewGridWithColumns(2, addBtn, refreshBtn),
	)
	content := container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), statusLbl, buttons),
		nil, nil,
		list,
	)

	d := dialog.NewCustom("Manage Accounts", "Close", content, w)
	d.Resize(fyne.NewSize(660, 480))
	d.Show()

	reload("", "")
}
