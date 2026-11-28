package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tg-dlp_GUI/internal/tgdlp"
)

// showAccountsDialog is the GUI version of the "tg-dlp accounts" menu.
//   proxyArgs: extra global flags (e.g. --proxy) used for the QR login
//   onChanged: called after the active account / account list changed
func showAccountsDialog(w fyne.Window, proxyArgs func() []string, onChanged func()) {
	var (
		items    []tgdlp.Account
		selected = -1
		busy     = true
		list     *widget.List
	)

	statusLbl := widget.NewLabel("Loading accounts...")
	statusLbl.Wrapping = fyne.TextWrapWord

	switchBtn := widget.NewButtonWithIcon("Switch to selected", theme.ConfirmIcon(), nil)
	switchBtn.Importance = widget.HighImportance
	addBtn := widget.NewButtonWithIcon(" Add account (QR login)", theme.ContentAddIcon(), nil)
	renameBtn := widget.NewButtonWithIcon("Rename profile", theme.DocumentCreateIcon(), nil)
	passwordBtn := widget.NewButtonWithIcon("Change password", theme.VisibilityIcon(), nil)
	removeBtn := widget.NewButtonWithIcon(" Remove", theme.ContentRemoveIcon(), nil)
	removeBtn.Importance = widget.DangerImportance
	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), nil)

	current := func() (tgdlp.Account, bool) {
		if selected < 0 || selected >= len(items) {
			return tgdlp.Account{}, false
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
		setEnabled(passwordBtn, !busy && ok)
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
			got, err := tgdlp.ListAccounts()
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
				msg := tgdlp.CleanCLIText(out)
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
			return tgdlp.RunHidden(20*time.Second, "switch", it.Profile)
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
				if !tgdlp.ProfileNameRe.MatchString(newName) {
					dialog.ShowError(errors.New("Use letters, digits, '-' and '_' (max 32 characters)."), w)
					return
				}
				run("Renaming...", newName, true, func() (string, error) {
					return tgdlp.RunHidden(20*time.Second, "rename", it.Profile, newName)
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
					fresh, err := tgdlp.ListAccounts()
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
					out, err := tgdlp.RemoveAccount(num)
					if err != nil {
						return out, err
					}
					after, lerr := tgdlp.ListAccounts()
					if lerr == nil && tgdlp.ProfileExists(after, it.Profile) {
						return out, errors.New("the account is still in the list")
					}
					return "Removed " + it.Label + " [profile: " + it.Profile + "]", nil
				})
			}, w)
	}

	// ---- Add (form -> config-pw -> config-cache -> login --qr-url shown as QR) ----
	addBtn.OnTapped = func() {
		showAddAccountDialog(w, items, proxyArgs, func(profile string, added bool, msg string) {
			reload(profile, msg)
			if added && onChanged != nil {
				onChanged()
			}
		})
	}

	// ---- Change session password / remember time ----
	passwordBtn.OnTapped = func() {
		it, ok := current()
		if !ok {
			return
		}
		showChangePasswordDialog(w, it, func(msg string) {
			reload(it.Profile, msg)
		})
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
		container.NewGridWithColumns(3, addBtn, passwordBtn, refreshBtn),
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
