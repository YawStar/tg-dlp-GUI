package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tg-dlp_GUI/internal/persist"
)

// only one history window at a time
var historyWin fyne.Window

func statusTag(s string) string {
	switch s {
	case "finished":
		return "[OK]"
	case "failed":
		return "[FAIL]"
	case "cancelled":
		return "[STOP]"
	case "running":
		return "[RUN]"
	case "interrupted":
		return "[CRASH]"
	}
	return "[?]"
}

func entryDuration(e persist.HistoryEntry) string {
	if e.Ended.IsZero() || e.Ended.Before(e.Started) {
		return "-"
	}
	d := e.Ended.Sub(e.Started).Round(time.Second)
	return d.String()
}

// newReadOnlyText returns a multi-line entry the user can select/copy from but not edit.
func newReadOnlyText() (*widget.Entry, func(string)) {
	e := widget.NewMultiLineEntry()
	cur := ""
	e.OnChanged = func(t string) {
		if t != cur {
			e.SetText(cur) // revert user edits
		}
	}
	return e, func(s string) {
		cur = s
		e.SetText(s)
	}
}

// showHistoryWindow opens (or focuses) the task history window.
// onLoad is called when the user presses "Load into form".
func showHistoryWindow(a fyne.App, store *persist.HistoryStore, onLoad func(persist.HistoryEntry)) {
	if historyWin != nil {
		historyWin.RequestFocus()
		return
	}

	hw := a.NewWindow("Task History")
	historyWin = hw
	hw.Resize(fyne.NewSize(980, 620))

	filters := []string{
		"All", "Download (dl)", "Upload (up)", "Export (export)", "Backup (backup)",
		"Restore (restore)", "Info (info)", "Delete (delete)", "Purge (purge)",
	}
	filter := "All"

	var (
		shown    []persist.HistoryEntry
		selected = -1
		list     *widget.List
	)

	// ---- detail pane ----
	cmdLbl := widget.NewLabel("-")
	targetLbl := widget.NewLabel("-")
	targetLbl.Wrapping = fyne.TextWrapBreak
	statusLbl := widget.NewLabel("-")
	timeLbl := widget.NewLabel("-")
	sumLbl := widget.NewLabel("-")
	cmdLineLbl := widget.NewLabel("-")
	cmdLineLbl.Wrapping = fyne.TextWrapBreak

	detail := widget.NewForm(
		widget.NewFormItem("Command", cmdLbl),
		widget.NewFormItem("Target", targetLbl),
		widget.NewFormItem("Status", statusLbl),
		widget.NewFormItem("Time", timeLbl),
		widget.NewFormItem("Result", sumLbl),
		widget.NewFormItem("Command line", cmdLineLbl),
	)

	logView, setLog := newReadOnlyText()
	logView.SetPlaceHolder("Select a task on the left to see its log")

	clearDetail := func() {
		cmdLbl.SetText("-")
		targetLbl.SetText("-")
		statusLbl.SetText("-")
		timeLbl.SetText("-")
		sumLbl.SetText("-")
		cmdLineLbl.SetText("-")
		setLog("")
	}

	showDetail := func(e persist.HistoryEntry) {
		cmdLbl.SetText(e.Command)
		targetLbl.SetText(e.Target)
		st := e.Status
		if e.Status != "running" && e.Status != "interrupted" {
			st = fmt.Sprintf("%s (exit code %d)", e.Status, e.ExitCode)
		}
		statusLbl.SetText(st)
		timeLbl.SetText(fmt.Sprintf("%s  (took %s)", e.Started.Format("2006-01-02 15:04:05"), entryDuration(e)))
		if e.Summary == "" {
			sumLbl.SetText("-")
		} else {
			sumLbl.SetText(e.Summary)
		}
		cmdLineLbl.SetText(e.CmdLine)
		if e.LogFile == "" {
			setLog("(no log file)")
			return
		}
		txt, err := persist.ReadLogTail(e.LogFile, 512*1024)
		if err != nil {
			setLog("(log file not found - it may have been deleted)")
			return
		}
		setLog(txt)
	}

	current := func() (persist.HistoryEntry, bool) {
		if selected < 0 || selected >= len(shown) {
			return persist.HistoryEntry{}, false
		}
		return shown[selected], true
	}

	// ---- list ----
	reload := func() {
		prevID := ""
		if e, ok := current(); ok {
			prevID = e.ID
		}
		all := store.List()
		shown = shown[:0]
		for _, e := range all {
			if filter == "All" || e.Command == filter {
				shown = append(shown, e)
			}
		}
		selected = -1
		for i, e := range shown {
			if e.ID == prevID {
				selected = i
				break
			}
		}
		list.Refresh()
		if selected >= 0 {
			list.Select(selected)
		} else {
			list.UnselectAll()
			clearDetail()
		}
	}

	list = widget.NewList(
		func() int { return len(shown) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("template template template template")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(shown) {
				return
			}
			e := shown[id]
			target := e.Target
			if len(target) > 40 {
				target = target[:37] + "..."
			}
			o.(*widget.Label).SetText(fmt.Sprintf("%-7s %s  %s  %s",
				statusTag(e.Status), e.Started.Format("01-02 15:04"), persist.CmdShortName(e.Command), target))
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		selected = id
		if id < len(shown) {
			showDetail(shown[id])
		}
	}
	list.OnUnselected = func(id widget.ListItemID) {}

	filterSel := widget.NewSelect(filters, func(s string) {
		filter = s
		reload()
	})
	filterSel.SetSelected("All")

	// ---- buttons ----
	loadBtn := widget.NewButtonWithIcon("Load into form", theme.UploadIcon(), func() {
		if e, ok := current(); ok {
			onLoad(e)
		}
	})
	copyCmdBtn := widget.NewButtonWithIcon("Copy command", theme.ContentCopyIcon(), func() {
		if e, ok := current(); ok {
			hw.Clipboard().SetContent(e.CmdLine)
		}
	})
	openLogBtn := widget.NewButtonWithIcon("Open log file", theme.DocumentIcon(), func() {
		if e, ok := current(); ok && e.LogFile != "" {
			if err := openLocalPath(a, filepath.Join(persist.LogsDir(), e.LogFile)); err != nil {
				dialog.ShowError(err, hw)
			}
		}
	})
	delBtn := widget.NewButtonWithIcon("Delete entry", theme.DeleteIcon(), func() {
		if e, ok := current(); ok {
			dialog.ShowConfirm("Delete entry", "Delete this history entry and its log file?", func(ok bool) {
				if ok {
					store.Delete(e.ID)
				}
			}, hw)
		}
	})
	folderBtn := widget.NewButtonWithIcon("Data folder", theme.FolderOpenIcon(), func() {
		if err := openLocalPath(a, persist.AppDataDir()); err != nil {
			dialog.ShowError(err, hw)
		}
	})
	clearAllBtn := widget.NewButtonWithIcon("Clear all", theme.DeleteIcon(), func() {
		dialog.ShowConfirm("Clear history", "Delete ALL history entries and their log files?", func(ok bool) {
			if ok {
				store.Clear()
			}
		}, hw)
	})

	left := container.NewBorder(
		container.NewBorder(nil, nil, widget.NewLabel("Show:"), nil, filterSel),
		container.NewHBox(folderBtn, clearAllBtn),
		nil, nil,
		list,
	)

	right := container.NewBorder(
		container.NewVBox(
			detail,
			container.NewHBox(loadBtn, copyCmdBtn, openLogBtn, delBtn),
			widget.NewLabel("Log:"),
		),
		nil, nil, nil,
		logView,
	)

	split := container.NewHSplit(left, right)
	split.Offset = 0.38
	hw.SetContent(split)

	// live refresh while tasks start/finish
	store.SetOnChange(func() { fyne.Do(reload) })

	hw.SetOnClosed(func() {
		store.SetOnChange(nil)
		historyWin = nil
	})

	reload()
	hw.Show()
}
