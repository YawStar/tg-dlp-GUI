package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tg-dlp_GUI/assets"
	"tg-dlp_GUI/internal/persist"
	"tg-dlp_GUI/internal/platform"
	"tg-dlp_GUI/internal/progress"
	"tg-dlp_GUI/internal/tgdlp"
)

var exePathGlobal string

type pendEntry struct {
	key  string
	text string
}

// Run builds the main window and blocks until the application quits.
func Run() {
	// The app ID is what Windows shows in the notification header.
	a := app.NewWithID("io.github.yawstar.tg-dlp-gui")
	w := a.NewWindow("tg-dlp GUI (Fyne)")

	// Last used configuration + task history (stored in the user's config dir)
	cfg, haveCfg := persist.LoadConfig()
	historyDB := persist.OpenHistory()

	if cfg.WinW >= 600 && cfg.WinH >= 450 {
		w.Resize(fyne.NewSize(cfg.WinW, cfg.WinH))
	} else {
		w.Resize(fyne.NewSize(820, 650))
	}

	if len(assets.AppIcon) > 0 {
		iconRes := fyne.NewStaticResource("app-icon.png", assets.AppIcon)
		a.SetIcon(iconRes) // also used as the notification icon
		w.SetIcon(iconRes)
	}

	// --------------------------------------------------------
	// Theme & Top-Right Quick Action Buttons
	// --------------------------------------------------------

	isDarkTheme := true
	darkIcons := buildIconSet(true)
	lightIcons := buildIconSet(false)

	activeIcons := func() iconSet {
		if isDarkTheme {
			return darkIcons
		}
		return lightIcons
	}

	darkPrimaryLabelColor := color.NRGBA{R: 0, G: 174, B: 255, A: 255}
	lightPrimaryLabelColor := color.NRGBA{R: 15, G: 92, B: 184, A: 255}

	darkSecondaryLabelColor := color.NRGBA{R: 233, G: 181, B: 0, A: 255}
	lightSecondaryLabelColor := color.NRGBA{R: 161, G: 98, B: 7, A: 255}

	primaryFieldLabel := canvas.NewText("Telegram URL (-u):", darkPrimaryLabelColor)
	primaryFieldLabel.TextSize = theme.TextSize()

	secondaryFieldLabel := canvas.NewText("Output Dir (-d):", darkSecondaryLabelColor)
	secondaryFieldLabel.TextSize = theme.TextSize()

	makeCircleIconBtn := func(iconRes fyne.Resource, onTapped func()) (*canvas.Image, fyne.CanvasObject) {
		bg := canvas.NewCircle(color.Transparent)
		iconImg := canvas.NewImageFromResource(iconRes)
		iconImg.FillMode = canvas.ImageFillContain
		iconImg.SetMinSize(fyne.NewSize(20, 20))

		hovered := false
		updateCircle := func() {
			if hovered {
				if isDarkTheme {
					bg.FillColor = color.NRGBA{R: 255, G: 255, B: 255, A: 28}
				} else {
					bg.FillColor = color.NRGBA{R: 17, G: 24, B: 39, A: 24}
				}
			} else {
				bg.FillColor = color.Transparent
			}
			bg.Refresh()
		}

		tapTarget := newHoverBox(nil, func(h bool) {
			hovered = h
			fyne.Do(updateCircle)
		})
		tapTarget.onTap = onTapped

		view := container.NewStack(bg, container.NewPadded(iconImg), tapTarget)
		return iconImg, view
	}

	var themeImg, repoImg, infoImg, settingsImg, heartImg *canvas.Image
	var themeView, repoView, infoView, settingsView, heartView fyne.CanvasObject

	applyTheme := func(dark bool) {
		isDarkTheme = dark
		icons := activeIcons()
		if isDarkTheme {
			a.Settings().SetTheme(theme.DarkTheme())
			themeImg.Resource = icons.sun
			primaryFieldLabel.Color = darkPrimaryLabelColor
			secondaryFieldLabel.Color = darkSecondaryLabelColor
		} else {
			a.Settings().SetTheme(theme.LightTheme())
			themeImg.Resource = icons.moon
			primaryFieldLabel.Color = lightPrimaryLabelColor
			secondaryFieldLabel.Color = lightSecondaryLabelColor
		}
		primaryFieldLabel.Refresh()
		secondaryFieldLabel.Refresh()
		repoImg.Resource = icons.github
		infoImg.Resource = icons.info
		settingsImg.Resource = icons.settings
		heartImg.Resource = icons.heart
		themeImg.Refresh()
		repoImg.Refresh()
		heartImg.Refresh()
		infoImg.Refresh()
		settingsImg.Refresh()
		if content := w.Canvas().Content(); content != nil {
			content.Refresh()
		}
	}

	themeImg, themeView = makeCircleIconBtn(activeIcons().sun, func() {
		applyTheme(!isDarkTheme)
	})

	repoImg, repoView = makeCircleIconBtn(activeIcons().github, func() {
		if u, err := url.Parse(AppRepoURL); err == nil {
			_ = a.OpenURL(u)
		}
	})

	infoImg, infoView = makeCircleIconBtn(activeIcons().info, func() {
		showAboutDialog(a, w)
	})

	var openSettingsAndAccountsDialog func()
	settingsImg, settingsView = makeCircleIconBtn(activeIcons().settings, func() {
		if openSettingsAndAccountsDialog != nil {
			openSettingsAndAccountsDialog()
		}
	})

	heartImg, heartView = makeCircleIconBtn(activeIcons().heart, func() {
		if u, err := url.Parse("https://yawstardancebox.github.io/donate"); err == nil {
			_ = a.OpenURL(u)
		}
	})

	topRightActions := container.NewHBox(
		themeView,
		heartView,
		repoView,
		settingsView,
		infoView,
	)

	// --------------------------------------------------------
	// Command & Field Controls
	// --------------------------------------------------------

	commandSelect := widget.NewSelect(
		[]string{
			"Download (dl)",
			"Upload (up)",
			"Export (export)",
			"Backup (backup)",
			"Restore (restore)",
			"Info (info)",
			"Delete (delete)",
			"Purge (purge)",
		},
		nil,
	)
	commandSelect.SetSelected("Download (dl)")

	// Primary input: URL (-u), File path (-p), Restore JSON file (-f), or Channel/Chat (-c)
	primaryEntry := widget.NewEntry()
	primaryEntry.SetPlaceHolder("Telegram link (-u, e.g. https://t.me/channel/123)")

	// Secondary input: Output Dir (-d) or Target Chat (-c)
	secondaryEntry := widget.NewEntry()
	secondaryEntry.SetPlaceHolder("Output directory (-d, default: downloads)")
	secondaryEntry.SetText("downloads")

	// Tertiary input: Limit Rate (-r), Include Exts (-i), Limit (-l), or Output ZIP (-o)
	tertiaryEntry := widget.NewEntry()
	tertiaryEntry.SetPlaceHolder("Speed limit (-r, e.g. 500K, 1M, optional)")

	// Quaternary input: Filename Template (-t), Exclude Exts (-e), or Filter (-i)
	quaternaryEntry := widget.NewEntry()
	quaternaryEntry.SetPlaceHolder("Filename template (-t, e.g. {id}_{ext}, optional)")

	// Concurrency (-c for dl)
	concurrencyEntry := widget.NewEntry()
	concurrencyEntry.SetPlaceHolder("Concurrency (--concurrency, e.g. 5, optional)")

	// Global Proxy & Custom Args
	proxyEntry := widget.NewEntry()
	proxyEntry.SetPlaceHolder("Proxy URL (--proxy, e.g. http://127.0.0.1:7890)")

	customArgEntry := widget.NewEntry()
	customArgEntry.SetPlaceHolder("Extra raw args (optional)")

	// Global & Command Checkboxes
	noProxyCheck := widget.NewCheck("No Proxy (--no-proxy)", nil)
	skipFloodCheck := widget.NewCheck("Skip Flood (--skip-flood)", nil)
	debugCheck := widget.NewCheck("Debug (--debug)", nil)

	// Download options
	skipSameCheck := widget.NewCheck("Skip Same (--skip-same)", nil)
	overwriteCheck := widget.NewCheck("Overwrite (--overwrite)", nil)
	takeoutCheck := widget.NewCheck("Takeout (--takeout)", nil)
	autoJoinCheck := widget.NewCheck("Auto Join (--auto-join)", nil)
	dryRunCheck := widget.NewCheck("Dry Run (--dry-run)", nil)

	// Upload options
	photoCheck := widget.NewCheck("As Photo (--photo)", nil)
	rmAfterUploadCheck := widget.NewCheck("Remove Local (--rm)", nil)

	// "Browse" button next to the Output Dir field (shown for Download / Export only)
	browseOutBtn := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		d := dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if dir == nil {
				return // cancelled
			}
			secondaryEntry.SetText(filepath.FromSlash(dir.Path()))
		}, w)
		// start in the folder that is already typed in (if it exists)
		if p := strings.TrimSpace(secondaryEntry.Text); p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				if st, err := os.Stat(abs); err == nil && st.IsDir() {
					if lister, err := storage.ListerForURI(storage.NewFileURI(abs)); err == nil {
						d.SetLocation(lister)
					}
				}
			}
		}
		d.Resize(fyne.NewSize(720, 500))
		d.Show()
	})

	// Info / Purge options
	saveInfoCheck := widget.NewCheck("Save Info (--save)", nil)
	purgeTextCheck := widget.NewCheck("Text Only (--text)", nil)
	purgeMediaCheck := widget.NewCheck("Media Only (--media)", nil)
	purgeYesCheck := widget.NewCheck("Skip Confirm (-y)", nil)

	// --------------------------------------------------------
	// Synchronization & State
	// --------------------------------------------------------

	var (
		mu       sync.Mutex
		cancel   context.CancelFunc
		killNow  func() // set only if a stop failed, so a 2nd click can force-kill again
		logMu    sync.Mutex
		hasLogin bool
		stopping atomic.Bool
		sawTotal atomic.Bool // true once tg-dlp prints "Total ..." lines (bulk mode)

		// bulk-mode aggregate stats (protected by pendMu)
		activeSpeed    = map[string]float64{}
		firstTotalAt   time.Time
		firstTotalDone int
		aggETA         string

		// batched UI update state
		pendMu     sync.Mutex
		pendLog    []pendEntry
		pendIdx    map[string]int
		pendTotal  *progress.Progress
		pendItem   progress.Progress
		pendStatus string
		pendDirty  bool
		logLines   []string // protected by logMu
	)

	// --------------------------------------------------------
	// Progress & Log Components
	// --------------------------------------------------------

	progressBar := widget.NewProgressBar()
	progressBar.SetValue(0)
	barText := "0%"
	progressBar.TextFormatter = func() string { return barText }

	infiniteBar := widget.NewProgressBarInfinite()
	infiniteBar.Stop()
	infiniteBar.Hide()

	pctLabel := widget.NewLabel("N/A")
	speedLabel := widget.NewLabel("Speed: -")
	etaLabel := widget.NewLabel("ETA: -")
	sizeLabel := widget.NewLabel("Size: -")
	fileLabel := widget.NewLabel("File: -")
	statusLabel := widget.NewLabel("Ready")

	logBox := widget.NewMultiLineEntry()
	logBox.SetPlaceHolder("tg-dlp output logs will appear here...")

	// clearLog empties the log view and everything still queued for it.
	// It is called by the "Clear Log" button and at the start of every task.
	// Safe to call from any goroutine.
	clearLog := func() {
		pendMu.Lock()
		pendLog, pendIdx = nil, nil
		pendMu.Unlock()

		logMu.Lock()
		logLines = nil
		logMu.Unlock()

		fyne.Do(func() {
			logBox.SetText("")
			logBox.CursorRow = 0
		})
	}

	clearLogBtn := widget.NewButton("Clear Log", clearLog)
	clearLogBtn.Importance = widget.LowImportance

	// What to do when a task succeeds (saved in the config).
	afterSelect := newAfterTaskSelect(cfg.AfterTask)
	var quitApp func() // assigned with the window close handler below

	resetStats := func() {
		fyne.Do(func() {
			barText = "0%"
			progressBar.SetValue(0)
			progressBar.Show()

			infiniteBar.Stop()
			infiniteBar.Hide()

			pctLabel.SetText("N/A")
			speedLabel.SetText("Speed: -")
			etaLabel.SetText("ETA: -")
			sizeLabel.SetText("Size: -")
			fileLabel.SetText("File: -")
			statusLabel.SetText("Starting...")
			platform.ClearTaskbarProgress(w)
		})
	}

	updateProgress := func(prog progress.Progress, isTotal bool) {
		if isTotal {
			if prog.HasPercent {
				barText = prog.PctText
				if strings.Contains(prog.SizeText, "files") {
					barText = prog.PctText + "  (" + prog.SizeText + ")"
				}
				progressBar.SetValue(prog.Percent)
				pctLabel.SetText(prog.PctText)
				progressBar.Show()
				infiniteBar.Stop()
				infiniteBar.Hide()
				if !stopping.Load() {
					statusLabel.SetText(fmt.Sprintf("Running (%s)", prog.PctText))
				}
				platform.SetTaskbarProgress(w, prog.Percent)
			}
			if prog.SizeText != "" {
				sizeLabel.SetText("Size: " + prog.SizeText)
			}
		} else {
			if prog.FileName != "" {
				fileLabel.SetText("File: " + prog.FileName)
			}
			if prog.SpeedText != "" {
				speedLabel.SetText("Speed: " + prog.SpeedText)
			}
			if prog.ETAText != "" {
				cleanETA := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prog.ETAText), "ETA:"))
				cleanETA = strings.TrimSpace(strings.TrimPrefix(cleanETA, "ETA"))
				etaLabel.SetText("ETA: " + cleanETA)
			}
		}
	}

	appendLogAndStats := func(line string) {
		clean := strings.TrimSpace(progress.StripANSI(line))
		if clean == "" {
			return
		}
		prog := progress.ParseLine(clean)
		isTotal := strings.HasPrefix(clean, "Total")
		// single-file mode prints a bare bar line (no file name, no "Total")
		isBar := !isTotal && prog.HasPercent && strings.HasPrefix(clean, "[")
		// per-file line ("name [###] 12% | ...")
		isFileLine := !isTotal && !isBar && prog.HasPercent
		if isTotal {
			sawTotal.Store(true)
		}
		// Mode is decided from the output itself (not the URL):
		//  - bare bar line            -> single file, drives the main bar
		//  - "Total ..." line         -> bulk, drives the main bar
		//  - per-file line before any Total line -> fallback, drives the main bar
		drivesBar := isBar || (isFileLine && !sawTotal.Load())

		// history: keep the result summary, and write everything except
		// progress redraws to this task's log file (passwords masked)
		if (isTotal || drivesBar) && prog.SizeText != "" {
			persist.HistSummary.Store(prog.SizeText)
		}
		if !prog.HasPercent {
			persist.TaskLog.Load().Line(persist.RedactURLPassword(clean))
		}

		pendMu.Lock()
		// progress redraw lines of the same file are coalesced into one log line
		key := ""
		if prog.HasPercent {
			if isTotal {
				key = "Total"
			} else if isBar {
				key = "Bar"
			} else if i := strings.Index(clean, " ["); i > 0 {
				key = clean[:i]
			}
		}
		if key != "" {
			if idx, ok := pendIdx[key]; ok && idx < len(pendLog) {
				pendLog[idx].text = clean
			} else {
				if pendIdx == nil {
					pendIdx = map[string]int{}
				}
				pendIdx[key] = len(pendLog)
				pendLog = append(pendLog, pendEntry{key, clean})
			}
		} else {
			pendLog = append(pendLog, pendEntry{"", clean})
		}
		if isTotal {
			p := prog
			pendTotal = &p
			if m := progress.FilesRegex.FindStringSubmatch(prog.SizeText); len(m) == 3 {
				done, _ := strconv.Atoi(m[1])
				total, _ := strconv.Atoi(m[2])
				now := time.Now()
				if firstTotalAt.IsZero() {
					firstTotalAt = now
					firstTotalDone = done
				} else if el := now.Sub(firstTotalAt).Seconds(); el >= 5 && done > firstTotalDone {
					rate := float64(done-firstTotalDone) / el
					aggETA = "~" + progress.FormatDuration(int(float64(total-done)/rate))
				}
			}
		} else {
			if drivesBar {
				p := prog
				pendTotal = &p
			}
			if isFileLine && key != "" {
				if prog.Percent >= 1.0 {
					delete(activeSpeed, key)
				} else {
					activeSpeed[key] = progress.ParseRateBytes(prog.SpeedText)
				}
			}
			if strings.Contains(clean, "Downloaded:") && prog.FileName != "" {
				delete(activeSpeed, prog.FileName)
			}
			if prog.FileName != "" {
				pendItem.FileName = prog.FileName
			}
			if prog.SpeedText != "" {
				pendItem.SpeedText = prog.SpeedText
			}
			if prog.ETAText != "" {
				pendItem.ETAText = prog.ETAText
			}
		}
		if !prog.HasPercent {
			low := strings.ToLower(clean)
			switch {
			case strings.Contains(clean, "Sleeping for"),
				strings.Contains(low, "flood"),
				strings.HasPrefix(clean, "Downloaded:"),
				strings.HasPrefix(clean, "Uploaded:"):
				pendStatus = clean
			case strings.Contains(clean, "All tasks completed"):
				pendStatus = "All tasks completed!"
			}
		}
		pendDirty = true
		pendMu.Unlock()
	}

	// Single flusher: updates the UI at most once every 150ms
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			pendMu.Lock()
			if !pendDirty {
				pendMu.Unlock()
				continue
			}
			lines, total, item, status := pendLog, pendTotal, pendItem, pendStatus
			pendLog, pendTotal, pendItem, pendStatus, pendDirty = nil, nil, progress.Progress{}, "", false
			pendIdx = nil
			aggSpeedText, aggETAText := "", ""
			if sawTotal.Load() {
				sum := 0.0
				for _, v := range activeSpeed {
					sum += v
				}
				aggSpeedText = progress.FormatRate(sum)
				aggETAText = aggETA
			}
			pendMu.Unlock()
			if aggSpeedText != "" {
				item.SpeedText = aggSpeedText
			}
			if aggETAText != "" {
				item.ETAText = aggETAText
			}

			var text string
			var n int
			if len(lines) > 0 {
				logMu.Lock()
				for _, e := range lines {
					logLines = append(logLines, e.text)
				}
				if len(logLines) > 300 {
					logLines = logLines[len(logLines)-300:]
				}
				text = strings.Join(logLines, "\n") + "\n"
				n = len(logLines)
				logMu.Unlock()
			}

			fyne.Do(func() {
				if len(lines) > 0 {
					logBox.SetText(text)
					logBox.CursorRow = n
				}
				if total != nil {
					updateProgress(*total, true)
				}
				updateProgress(item, false)
				if status != "" && !stopping.Load() {
					statusLabel.SetText(status)
				}
			})
		}
	}()

	notifyDone := func(title string, content string) {
		fyne.Do(func() {
			a.SendNotification(fyne.NewNotification(title, content))
		})
	}

	// --------------------------------------------------------
	// Action Button (Start / Stop)
	// --------------------------------------------------------

	var (
		actionBtn   *widget.Button
		actionView  fyne.CanvasObject
		btnBg       *canvas.Rectangle
		btnLabel    *canvas.Text
		btnIcon     *canvas.Image
		isHovered   bool
	)

	// one icon per action label (see getActionLabel); the icon is always white on the coloured button
	actionIconRes := map[string]fyne.Resource{
		"Start Download": svgIconResource("action_download.svg", downloadSVG, true),
		"Start Upload":   svgIconResource("action_upload.svg", actUploadSVG, true),
		"Start Export":   svgIconResource("action_export.svg", actExportSVG, true),
		"Start Backup":   svgIconResource("action_backup.svg", actBackupSVG, true),
		"Start Restore":  svgIconResource("action_restore.svg", actRestoreSVG, true),
		"Fetch Info":     svgIconResource("action_info.svg", actInfoSVG, true),
		"Delete Post":    svgIconResource("action_delete.svg", actDeleteSVG, true),
		"Purge Channel":  svgIconResource("action_purge.svg", actPurgeSVG, true),
	}
	dlActionIconRes := actionIconRes["Start Download"]
	stopActionIconRes := svgIconResource("action_stop.svg", stopSVG, true)

	darkBlue := color.NRGBA{R: 21, G: 62, B: 117, A: 255}
	lightBlue := color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	darkBlueDisabled := color.NRGBA{R: 21, G: 62, B: 117, A: 140}

	darkRed := color.NRGBA{R: 127, G: 29, B: 29, A: 255}
	lightRed := color.NRGBA{R: 220, G: 38, B: 38, A: 255}
	darkRedDisabled := color.NRGBA{R: 127, G: 29, B: 29, A: 140}

	textEnabledColor := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	textDisabledColor := color.NRGBA{R: 255, G: 255, B: 255, A: 170}

	getActionLabel := func(cmdMode string, running bool) string {
		if running {
			return "Stop Task"
		}
		switch cmdMode {
		case "Upload (up)":
			return "Start Upload"
		case "Export (export)":
			return "Start Export"
		case "Backup (backup)":
			return "Start Backup"
		case "Restore (restore)":
			return "Start Restore"
		case "Info (info)":
			return "Fetch Info"
		case "Delete (delete)":
			return "Delete Post"
		case "Purge (purge)":
			return "Purge Channel"
		default:
			return "Start Download"
		}
	}

	applyBtnColor := func(text string, enabled bool) {
		if btnBg == nil {
			return
		}
		if strings.HasPrefix(text, "Stop") || strings.HasPrefix(text, "Purge") || strings.HasPrefix(text, "Delete") {
			if !enabled {
				btnBg.FillColor = darkRedDisabled
			} else if isHovered {
				btnBg.FillColor = lightRed
			} else {
				btnBg.FillColor = darkRed
			}
		} else {
			if !enabled {
				btnBg.FillColor = darkBlueDisabled
			} else if isHovered {
				btnBg.FillColor = lightBlue
			} else {
				btnBg.FillColor = darkBlue
			}
		}

		if btnIcon != nil {
			if strings.HasPrefix(text, "Stop") {
				btnIcon.Resource = stopActionIconRes
			} else if res, ok := actionIconRes[text]; ok {
				btnIcon.Resource = res
			} else {
				btnIcon.Resource = dlActionIconRes
			}
			if enabled {
				btnIcon.Translucency = 0
			} else {
				btnIcon.Translucency = 0.35
			}
			btnIcon.Refresh()
		}

		if btnLabel != nil {
			btnLabel.Text = text
			if enabled {
				btnLabel.Color = textEnabledColor
			} else {
				btnLabel.Color = textDisabledColor
			}
			btnLabel.Refresh()
		}
		btnBg.Refresh()
	}

	setActionState := func(text string, enabled bool) {
		actionBtn.SetText("")

		// Button state follows task state only (idle / running / stopping).
		// While a stop is in progress the button stays disabled no matter
		// who calls setActionState; it is re-enabled when the process has
		// really exited (cancel == nil).
		mu.Lock()
		stoppingNow := cancel != nil && stopping.Load()
		mu.Unlock()

		finalEnabled := enabled && !stoppingNow
		if finalEnabled {
			actionBtn.Enable()
		} else {
			actionBtn.Disable()
		}
		applyBtnColor(text, finalEnabled)
	}

	// --------------------------------------------------------
	// Dynamic Field Visibility by Selected Command
	// --------------------------------------------------------

	tertiaryLabel := widget.NewLabel("Speed Limit (-r):")
	quaternaryLabel := widget.NewLabel("Template (-t):")
	// Download only: the filename template (-t) has its own field next to the filter (-i).
	templateLabel := widget.NewLabel("Template (-t):")
	templateEntry := widget.NewEntry()
	templateEntry.SetPlaceHolder("e.g. {id}_{name}")
	templateBox := makeClearableEntry(templateEntry)
	templateLabel.Hide()
	templateBox.Hide()
	concurrencyLabel := widget.NewLabel("Concurrency (-c):")

	dlOptsRow := container.NewHBox(skipSameCheck, overwriteCheck, takeoutCheck, autoJoinCheck, dryRunCheck)
	upOptsRow := container.NewHBox(photoCheck, rmAfterUploadCheck)
	infoOptsRow := container.NewHBox(saveInfoCheck)
	purgeOptsRow := container.NewHBox(purgeTextCheck, purgeMediaCheck, purgeYesCheck)

	var syncCommandUI func(selected string)
	syncCommandUI = func(selected string) {
		mu.Lock()
		running := cancel != nil
		mu.Unlock()

		browseOutBtn.Hide()
		dlOptsRow.Hide()
		upOptsRow.Hide()
		infoOptsRow.Hide()
		purgeOptsRow.Hide()
		concurrencyEntry.Show()
		concurrencyLabel.Show()
		quaternaryEntry.Show()
		quaternaryLabel.Show()
		tertiaryEntry.Show()
		tertiaryLabel.Show()
		templateLabel.Hide()
		templateBox.Hide()

		switch selected {
		case "Upload (up)":
			primaryFieldLabel.Text = "File / Dir Path (-p):"
			primaryEntry.SetPlaceHolder("File or directory path (-p, e.g. ./video.mp4)")
			secondaryFieldLabel.Text = "Target Chat (-c):"
			secondaryEntry.SetPlaceHolder("Target chat (-c, default: Saved Messages)")
			if secondaryEntry.Text == "downloads" || secondaryEntry.Text == "exports" {
				secondaryEntry.SetText("")
			}
			tertiaryLabel.SetText("Include Exts (-i):")
			tertiaryEntry.SetPlaceHolder("Include extensions (-i, e.g. mp4,jpg)")
			quaternaryLabel.SetText("Exclude Exts (-e):")
			quaternaryEntry.SetPlaceHolder("Exclude extensions (-e, e.g. tmp,log)")
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()
			upOptsRow.Show()

		case "Export (export)":
			primaryFieldLabel.Text = "Channel Link (-u):"
			primaryEntry.SetPlaceHolder("Telegram channel link (-u, required)")
			secondaryFieldLabel.Text = "Output Dir (-d):"
			secondaryEntry.SetPlaceHolder("Output directory (-d, default: exports)")
			if secondaryEntry.Text == "downloads" || secondaryEntry.Text == "" {
				secondaryEntry.SetText("exports")
			}
			tertiaryLabel.SetText("Limit (-l):")
			tertiaryEntry.SetPlaceHolder("Max messages (-l, 0 = all)")
			quaternaryLabel.Hide()
			quaternaryEntry.Hide()
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()
			browseOutBtn.Show()

		case "Backup (backup)":
			primaryFieldLabel.Text = "Source Dir (-d):"
			primaryEntry.SetPlaceHolder("Directory to backup (-d, default: exports)")
			if primaryEntry.Text == "" || strings.HasPrefix(primaryEntry.Text, "http") {
				primaryEntry.SetText("exports")
			}
			secondaryFieldLabel.Text = "Output ZIP (-o):"
			secondaryEntry.SetPlaceHolder("Output ZIP filename (-o, optional)")
			secondaryEntry.SetText("")
			tertiaryLabel.Hide()
			tertiaryEntry.Hide()
			quaternaryLabel.Hide()
			quaternaryEntry.Hide()
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()

		case "Restore (restore)":
			primaryFieldLabel.Text = "JSON File (-f):"
			primaryEntry.SetPlaceHolder("JSON file to restore (-f, required)")
			secondaryFieldLabel.Text = "Target Chat (-c):"
			secondaryEntry.SetPlaceHolder("Target channel or \"me\" for Saved Messages")
			secondaryEntry.SetText("me")
			tertiaryLabel.Hide()
			tertiaryEntry.Hide()
			quaternaryLabel.Hide()
			quaternaryEntry.Hide()
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()

		case "Info (info)":
			primaryFieldLabel.Text = "Telegram Link (-u):"
			primaryEntry.SetPlaceHolder("Telegram channel/post link (-u, e.g. https://t.me/channel/123)")
			secondaryFieldLabel.Text = "Save Target (-save):"
			secondaryEntry.SetPlaceHolder("Channel link for --save (optional)")
			if secondaryEntry.Text == "downloads" || secondaryEntry.Text == "exports" {
				secondaryEntry.SetText("")
			}
			tertiaryLabel.Hide()
			tertiaryEntry.Hide()
			quaternaryLabel.Hide()
			quaternaryEntry.Hide()
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()
			infoOptsRow.Show()

		case "Delete (delete)":
			primaryFieldLabel.Text = "Message URL (-u):"
			primaryEntry.SetPlaceHolder("Telegram message link (-u, required)")
			secondaryFieldLabel.Text = "Output Dir (-d):"
			secondaryEntry.SetPlaceHolder("Unused for delete")
			tertiaryLabel.Hide()
			tertiaryEntry.Hide()
			quaternaryLabel.Hide()
			quaternaryEntry.Hide()
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()

		case "Purge (purge)":
			primaryFieldLabel.Text = "Target Channel (-c):"
			primaryEntry.SetPlaceHolder("Target channel (-c, required, e.g. @mychannel)")
			secondaryFieldLabel.Text = "Output Dir (-d):"
			secondaryEntry.SetPlaceHolder("Unused for purge")
			tertiaryLabel.SetText("Include Exts (-i):")
			tertiaryEntry.SetPlaceHolder("Only delete extensions (-i, e.g. jpg,mp4)")
			quaternaryLabel.SetText("Exclude Exts (-e):")
			quaternaryEntry.SetPlaceHolder("Exclude extensions (-e)")
			concurrencyLabel.Hide()
			concurrencyEntry.Hide()
			purgeOptsRow.Show()

		default: // Download (dl)
			primaryFieldLabel.Text = "Telegram URL (-u):"
			primaryEntry.SetPlaceHolder("Telegram link (-u, e.g. https://t.me/channel/123)")
			secondaryFieldLabel.Text = "Output Dir (-d):"
			secondaryEntry.SetPlaceHolder("Output directory (-d, default: downloads)")
			if secondaryEntry.Text == "exports" || secondaryEntry.Text == "me" || secondaryEntry.Text == "" {
				secondaryEntry.SetText("downloads")
			}
			tertiaryLabel.SetText("Speed Limit (-r):")
			tertiaryEntry.SetPlaceHolder("Speed limit (-r, e.g. 500K, 1M, optional)")
			quaternaryLabel.SetText("Filter (-i):")
			quaternaryEntry.SetPlaceHolder("e.g. mp4,mkv")
			templateLabel.Show()
			templateBox.Show()
			concurrencyLabel.SetText("Concurrency:")
			concurrencyEntry.SetPlaceHolder("Workers (--concurrency, e.g. 5)")
			dlOptsRow.Show()
			browseOutBtn.Show()
		}

		primaryFieldLabel.Refresh()
		secondaryFieldLabel.Refresh()

		mu.Lock()
		loggedIn := hasLogin
		mu.Unlock()

		setActionState(getActionLabel(selected, running), loggedIn || running)
	}

	// --------------------------------------------------------
	// Remember last used values (one profile per command)
	// --------------------------------------------------------

	// Destructive flags are deliberately NOT remembered (Skip Confirm -y,
	// Remove Local --rm): they must be ticked again every time.
	profileChecks := map[string]*widget.Check{
		"skipSame":  skipSameCheck,
		"overwrite": overwriteCheck,
		"takeout":   takeoutCheck,
		"autoJoin":  autoJoinCheck,
		"dryRun":    dryRunCheck,
		"photo":     photoCheck,
		"save":      saveInfoCheck,
		"text":      purgeTextCheck,
		"media":     purgeMediaCheck,
	}

	captureProfile := func() persist.FormProfile {
		p := persist.FormProfile{
			Primary:     primaryEntry.Text,
			Secondary:   secondaryEntry.Text,
			Tertiary:    tertiaryEntry.Text,
			Quaternary:  quaternaryEntry.Text,
			Template:    templateEntry.Text,
			Concurrency: concurrencyEntry.Text,
			Extra:       customArgEntry.Text,
			Checks:      map[string]bool{},
		}
		for k, c := range profileChecks {
			p.Checks[k] = c.Checked
		}
		return p
	}

	applyProfile := func(p persist.FormProfile) {
		primaryEntry.SetText(p.Primary)
		secondaryEntry.SetText(p.Secondary)
		tertiaryEntry.SetText(p.Tertiary)
		quaternaryEntry.SetText(p.Quaternary)
		templateEntry.SetText(p.Template)
		concurrencyEntry.SetText(p.Concurrency)
		customArgEntry.SetText(p.Extra)
		for k, c := range profileChecks {
			c.SetChecked(p.Checks[k])
		}
	}

	profiles := cfg.Profiles
	curCmd := commandSelect.Selected

	commandSelect.OnChanged = func(sel string) {
		profiles[curCmd] = captureProfile() // remember the command we are leaving
		syncCommandUI(sel)
		if p, ok := profiles[sel]; ok {
			applyProfile(p)
		} else {
			// first visit: don't leak per-command values (e.g. a speed limit) from the previous command
			tertiaryEntry.SetText("")
			quaternaryEntry.SetText("")
			templateEntry.SetText("")
			concurrencyEntry.SetText("")
		}
		curCmd = sel
	}

	loadFromHistory := func(e persist.HistoryEntry) {
		if commandSelect.Selected != e.Command {
			commandSelect.SetSelected(e.Command)
		}
		if e.Form != nil {
			applyProfile(*e.Form)
		}
		statusLabel.SetText("Loaded settings from history")
		w.RequestFocus()
	}

	snapshotConfig := func() persist.AppConfig {
		profiles[curCmd] = captureProfile()
		c := persist.AppConfig{
			Command: commandSelect.Selected,
			Theme:   "dark",
			Proxy:   proxyEntry.Text,
			Global: map[string]bool{
				"noProxy":   noProxyCheck.Checked,
				"skipFlood": skipFloodCheck.Checked,
				"debug":     debugCheck.Checked,
			},
			Profiles:  profiles,
			AfterTask: afterSelect.Selected,
		}
		if !isDarkTheme {
			c.Theme = "light"
		}
		if sz := w.Canvas().Size(); sz.Width >= 600 && sz.Height >= 450 {
			c.WinW, c.WinH = sz.Width, sz.Height
		}
		return c
	}

	// --------------------------------------------------------
	// Generic Command Executor (ConPTY)
	// --------------------------------------------------------

	// Shows captured command output in a scrollable, copyable dialog
	showResultDialog := func(title, text string) {
		lbl := widget.NewLabel(text)
		lbl.Wrapping = fyne.TextWrapWord
		scroll := container.NewVScroll(lbl)
		scroll.SetMinSize(fyne.NewSize(520, 340))
		copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
			w.Clipboard().SetContent(text)
		})
		content := container.NewBorder(nil, copyBtn, nil, nil, scroll)
		d := dialog.NewCustom(title, "OK", content, w)
		d.Resize(fyne.NewSize(600, 480))
		d.Show()
	}

	startCommandExecution := func(args []string, taskTitle string) {
		// Claim the running slot first so a second click can never start a
		// second process.
		mu.Lock()
		if cancel != nil {
			mu.Unlock()
			return
		}
		ctx, c := context.WithCancel(context.Background())
		cancel = c
		killNow = nil
		stopping.Store(false)
		mu.Unlock()

		resetStats()
		clearLog() // every task starts with an empty log

		exePath, err := tgdlp.ResolvePath()
		// fmt.Println("[cmd] " + platform.BuildCommandLine(exePath, persist.RedactArgs(args)))

		if err != nil {
			mu.Lock()
			cancel = nil
			mu.Unlock()
			c()
			appendLogAndStats("[!] Error: " + err.Error())
			fyne.Do(func() {
				statusLabel.SetText("tg-dlp not found")
				platform.SetTaskbarError(w)
				setActionState(getActionLabel(commandSelect.Selected, false), hasLogin)
				showTgDlpNotFoundDialog(w)
			})
			return
		}
		exePathGlobal = exePath

		// ---- history: record this task and open its log file ----
		// (only the 8 main commands; settings helpers like Config Password / Logout are not recorded)
		recordHistory := false
		for _, o := range commandSelect.Options {
			if o == taskTitle {
				recordHistory = true
				break
			}
		}
		persist.TaskLog.Store(nil)
		hForm := captureProfile()
		hEntry := persist.HistoryEntry{
			ID:      time.Now().Format("20060102-150405.000") + "-" + persist.CmdShortName(taskTitle),
			Command: taskTitle,
			Target:  strings.TrimSpace(primaryEntry.Text),
			CmdLine: platform.BuildCommandLine("tg-dlp", persist.RedactArgs(args)),
			Started: time.Now(),
			Status:  "running",
			Form:    &hForm,
		}
		hEntry.LogFile = hEntry.ID + ".log"
		var hLog *persist.TaskLogger
		if recordHistory {
			historyDB.Add(hEntry)
			hLog = persist.NewTaskLogger(filepath.Join(persist.LogsDir(), hEntry.LogFile))
			persist.TaskLog.Store(hLog)
		}
		persist.HistSummary.Store("")
		var hOnce sync.Once
		finishHistory := func(status string, code int) {
			hOnce.Do(func() {
				persist.TaskLog.Store(nil)
				if !recordHistory {
					return
				}
				hLog.Close()
				e := hEntry
				e.Ended = time.Now()
				e.Status = status
				e.ExitCode = code
				e.Summary, _ = persist.HistSummary.Load().(string)
				historyDB.Update(e)
			})
		}

		stopping.Store(false)
		sawTotal.Store(false)
		pendMu.Lock()
		activeSpeed = map[string]float64{}
		firstTotalAt = time.Time{}
		firstTotalDone = 0
		aggETA = ""
		pendMu.Unlock()
		fyne.Do(func() {
			setActionState("Stop Task", true)
			progressBar.Hide()
			infiniteBar.Show()
			infiniteBar.Start()
			platform.SetTaskbarIndeterminate(w)
			statusLabel.SetText("Starting " + taskTitle + "...")
		})

		go func() {
			appendLogAndStats("[cmd] " + platform.BuildCommandLine(exePath, persist.RedactArgs(args)))
			appendLogAndStats("[info] Starting pseudo terminal...")

			pty, err := platform.StartPTY(exePath, args)
			if err != nil {
				appendLogAndStats("[error] ConPTY start failed: " + err.Error())
				finishHistory("failed", -1)
				mu.Lock()
				cancel = nil
				mu.Unlock()
				c()
				fyne.Do(func() {
					statusLabel.SetText("Unable to start tg-dlp")
					infiniteBar.Stop()
					infiniteBar.Hide()
					platform.SetTaskbarError(w)
					setActionState(getActionLabel(commandSelect.Selected, false), hasLogin)
				})
				return
			}

			var (
				closeOnce sync.Once
				finished  = make(chan struct{})
			)
			closePTY := func() {
				closeOnce.Do(func() {
					pty.Close()
				})
			}

			go func() {
				select {
				case <-ctx.Done():
					// 1) Ctrl+C (async so a blocking Write can't stall the escalation).
					// Short grace period: over ConPTY, Ctrl+C often never reaches
					// tg-dlp, and bulk downloads keep running meanwhile.
					go pty.Interrupt()
					select {
					case <-finished:
						return
					case <-time.After(300 * time.Millisecond):
					}

					// 2) force-kill the whole process tree
					pty.Kill()
					select {
					case <-finished:
						return
					case <-time.After(1000 * time.Millisecond):
					}

					// 3) close the PTY (unblocks Wait / the reader)
					closePTY()
					select {
					case <-finished:
						return
					case <-time.After(3 * time.Second):
					}

					// 4) stop failed: never leave the user locked out; let them retry
					appendLogAndStats("[warn] tg-dlp did not exit yet - press Stop Task again")
					mu.Lock()
					killNow = func() {
						pty.Kill()
						closePTY()
					}
					stopping.Store(false)
					mu.Unlock()
					fyne.Do(func() {
						statusLabel.SetText("Stop failed, try again")
						setActionState("Stop Task", true)
					})
				case <-finished:
					return
				}
			}()

			captureInfo := taskTitle == "Info (info)"
			var captured []string

			// Info shows its own result dialog, so only the other tasks notify.
			notifyTask := func(msg string) {
				if !captureInfo {
					notifyDone("tg-dlp GUI", msg)
				}
			}

			scanDone := make(chan struct{})
			go func() {
				defer close(scanDone)
				scanner := bufio.NewScanner(pty)
				scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
				scanner.Split(progress.ScanCRorLF)
				for scanner.Scan() {
					line := string(scanner.Bytes())
					appendLogAndStats(line)
					if captureInfo {
						if clean := strings.TrimSpace(progress.StripANSI(line)); clean != "" {
							captured = append(captured, clean)
						}
					}
				}
				if err := scanner.Err(); err != nil && ctx.Err() == nil {
					select {
					case <-finished:
						// PTY handle was closed after the process exited (benign)
					default:
						appendLogAndStats("[scanner error] " + err.Error())
					}
				}
			}()

			exitCode, waitErr := pty.Wait()
			close(finished)
			closePTY()
			<-scanDone

			mu.Lock()
			cancel = nil
			mu.Unlock()

			if ctx.Err() != nil {
				appendLogAndStats("[cancelled] Task stopped by user.")
				fyne.Do(func() {
					statusLabel.SetText(taskTitle + " cancelled")
					platform.ClearTaskbarProgress(w)
				})
				notifyTask(taskTitle+" cancelled.")
			} else if waitErr != nil {
				appendLogAndStats("[error] " + waitErr.Error())
				fyne.Do(func() {
					statusLabel.SetText(taskTitle + " failed")
					platform.SetTaskbarError(w)
				})
				notifyTask(taskTitle+" failed.")
			} else if exitCode != 0 {
				appendLogAndStats(fmt.Sprintf("[error] tg-dlp exited with code %d", exitCode))
				fyne.Do(func() {
					statusLabel.SetText(taskTitle + " failed")
					platform.SetTaskbarError(w)
				})
				notifyTask(taskTitle+" failed.")
			} else {
				fyne.Do(func() {
					barText = "100%"
					progressBar.SetValue(1.0)
					pctLabel.SetText("100.0%")
					etaLabel.SetText("ETA: 0s")
					statusLabel.SetText(taskTitle + " finished")
					platform.ClearTaskbarProgress(w)
				})
				appendLogAndStats("[done] All tasks completed!")
				notifyTask(taskTitle+" finished!")
			}

			hStatus := "finished"
			if ctx.Err() != nil {
				hStatus = "cancelled"
			} else if waitErr != nil || exitCode != 0 {
				hStatus = "failed"
			}
			finishHistory(hStatus, exitCode)

			// only the main commands (not Info or the settings helpers) and only on success
			runAfter := hStatus == "finished" && recordHistory && !captureInfo

			if captureInfo && ctx.Err() == nil && len(captured) > 0 {
				// drop the "Account: ..." header line (we only want the target's info)
				var shown []string
				for _, ln := range captured {
					if strings.Contains(ln, "Account:") {
						continue
					}
					shown = append(shown, ln)
				}
				// drop separator lines left at the top
				for len(shown) > 0 && strings.Trim(shown[0], "= ") == "" {
					shown = shown[1:]
				}
				if len(shown) == 0 {
					shown = captured
				}
				infoText := strings.Join(shown, "\n")
				infoTitle := "Info Result"
				if waitErr != nil || exitCode != 0 {
					infoTitle = "Info Failed"
				}
				fyne.Do(func() {
					showResultDialog(infoTitle, infoText)
				})
			}

			c()

			fyne.Do(func() {
				infiniteBar.Stop()
				infiniteBar.Hide()
				progressBar.Show()
				setActionState(getActionLabel(commandSelect.Selected, false), hasLogin)
				if runAfter {
					runAfterTask(a, w, afterSelect.Selected, quitApp)
				}
			})
		}()
	}

	// --------------------------------------------------------
	// Build Arguments from Current Form State
	// --------------------------------------------------------

	buildSelectedCommandArgs := func() ([]string, string, error) {
		sel := commandSelect.Selected
		pVal := strings.TrimSpace(primaryEntry.Text)
		sVal := strings.TrimSpace(secondaryEntry.Text)
		tVal := strings.TrimSpace(tertiaryEntry.Text)
		qVal := strings.TrimSpace(quaternaryEntry.Text)
		cVal := strings.TrimSpace(concurrencyEntry.Text)
		proxyVal := strings.TrimSpace(proxyEntry.Text)
		extraVal := strings.TrimSpace(customArgEntry.Text)

		var args []string
		taskName := sel

		switch sel {
		case "Upload (up)":
			if pVal == "" {
				return nil, taskName, fmt.Errorf("File or directory path (-p) is required for upload.")
			}
			args = append(args, "up", "-p", pVal)
			if sVal != "" {
				args = append(args, "-c", sVal)
			}
			if tVal != "" {
				args = append(args, "-i", tVal)
			}
			if qVal != "" {
				args = append(args, "-e", qVal)
			}
			if photoCheck.Checked {
				args = append(args, "--photo")
			}
			if rmAfterUploadCheck.Checked {
				args = append(args, "--rm")
			}

		case "Export (export)":
			if pVal == "" {
				return nil, taskName, fmt.Errorf("Telegram channel link (-u) is required for export.")
			}
			outDir := sVal
			if outDir == "" {
				outDir = "exports"
			}
			args = append(args, "export", "-u", pVal, "-d", filepath.Clean(outDir))
			if tVal != "" {
				if _, err := strconv.Atoi(tVal); err != nil {
					return nil, taskName, fmt.Errorf("Limit (-l) must be a valid integer.")
				}
				args = append(args, "-l", tVal)
			}

		case "Backup (backup)":
			srcDir := pVal
			if srcDir == "" {
				srcDir = "exports"
			}
			args = append(args, "backup", "-d", filepath.Clean(srcDir))
			if sVal != "" {
				args = append(args, "-o", sVal)
			}

		case "Restore (restore)":
			if pVal == "" {
				return nil, taskName, fmt.Errorf("JSON file (-f) is required for restore.")
			}
			args = append(args, "restore", "-f", pVal)
			if sVal != "" {
				args = append(args, "-c", sVal)
			}

		case "Info (info)":
			if pVal == "" && sVal == "" {
				return nil, taskName, fmt.Errorf("Telegram link (-u) or --save channel link is required.")
			}
			args = append(args, "info")
			if pVal != "" {
				args = append(args, "-u", pVal)
			}
			if saveInfoCheck.Checked {
				if sVal != "" {
					args = append(args, "--save", sVal)
				} else if pVal != "" {
					args = append(args, "--save", pVal)
				}
			} else if sVal != "" {
				args = append(args, "--save", sVal)
			}

		case "Delete (delete)":
			if pVal == "" {
				return nil, taskName, fmt.Errorf("Telegram message URL (-u) is required for delete.")
			}
			args = append(args, "delete", "-u", pVal)

		case "Purge (purge)":
			if pVal == "" {
				return nil, taskName, fmt.Errorf("Target channel (-c) is required for purge.")
			}
			args = append(args, "purge", "-c", pVal)
			if tVal != "" {
				args = append(args, "-i", tVal)
			}
			if qVal != "" {
				args = append(args, "-e", qVal)
			}
			if purgeTextCheck.Checked {
				args = append(args, "--text")
			}
			if purgeMediaCheck.Checked {
				args = append(args, "--media")
			}
			if purgeYesCheck.Checked {
				args = append(args, "-y")
			}

		default: // Download (dl)
			if pVal == "" {
				return nil, taskName, fmt.Errorf("Telegram URL (-u) is required.")
			}
			outDir := sVal
			if outDir == "" {
				outDir = "downloads"
			}
			args = append(args, "dl", "-u", pVal, "-d", filepath.Clean(outDir))
			if tVal != "" {
				args = append(args, "-r", tVal)
			}
			filterVal, tmplVal := splitFilterTemplate(qVal, strings.TrimSpace(templateEntry.Text))
			if filterVal != "" {
				args = append(args, "-i", filterVal)
			}
			if tmplVal != "" {
				if err := validateFilenameTemplate(tmplVal); err != nil {
					return nil, taskName, err
				}
				args = append(args, "-t", tmplVal)
			}
			if cVal != "" {
				if _, err := strconv.Atoi(cVal); err != nil {
					return nil, taskName, fmt.Errorf("Concurrency must be an integer.")
				}
				args = append(args, "--concurrency", cVal)
			}
			if skipSameCheck.Checked {
				args = append(args, "--skip-same")
			}
			if overwriteCheck.Checked {
				args = append(args, "--overwrite")
			}
			if takeoutCheck.Checked {
				args = append(args, "--takeout")
			}
			if autoJoinCheck.Checked {
				args = append(args, "--auto-join")
			}
			if dryRunCheck.Checked {
				args = append(args, "--dry-run")
			}
		}

		// Global options
		if proxyVal != "" {
			args = append(args, "--proxy", proxyVal)
		} else if noProxyCheck.Checked {
			args = append(args, "--no-proxy")
		}
		if skipFloodCheck.Checked {
			args = append(args, "--skip-flood")
		}
		if debugCheck.Checked {
			args = append(args, "--debug")
		}
		if extraVal != "" {
			args = append(args, tgdlp.SplitCustomArgs(extraVal)...)
		}

		return args, taskName, nil
	}

	// --------------------------------------------------------
	// Action Button Click Handler
	// --------------------------------------------------------

	actionBtn = widget.NewButton("", func() {
		// State: idle (cancel == nil) -> running -> stopping (stopping == true)
		// -> idle again once the process has really exited.
		mu.Lock()
		running := cancel != nil
		cFunc := cancel
		kFunc := killNow
		alreadyStopping := stopping.Load()
		if running && !alreadyStopping {
			stopping.Store(true)
		}
		mu.Unlock()

		if running {
			if alreadyStopping {
				return // stop already in progress, button stays disabled
			}
			fyne.Do(func() {
				setActionState("Stop Task", false)
				statusLabel.SetText("Stopping...")
			})
			if cFunc != nil {
				cFunc()
			}
			if kFunc != nil {
				go kFunc() // retry after a failed stop: force-kill again
			}
			return
		}

		args, taskName, err := buildSelectedCommandArgs()
		if err != nil {
			appendLogAndStats("[!] Error: " + err.Error())
			fyne.Do(func() {
				statusLabel.SetText(err.Error())
				dialog.ShowError(err, w)
				setActionState(getActionLabel(commandSelect.Selected, false), hasLogin)
			})
			return
		}

		if commandSelect.Selected == "Purge (purge)" && !purgeYesCheck.Checked {
			dialog.ShowConfirm(
				"Confirm Channel Purge",
				fmt.Sprintf("Are you sure you want to purge messages in %q?\nThis action cannot be undone.", strings.TrimSpace(primaryEntry.Text)),
				func(confirmed bool) {
					if !confirmed {
						return
					}
					purgeArgs := append(args, "-y")
					startCommandExecution(purgeArgs, taskName)
				},
				w,
			)
			return
		}

		if commandSelect.Selected == "Delete (delete)" {
			dialog.ShowConfirm(
				"Confirm Delete Message",
				fmt.Sprintf("Delete message at:\n%s ?", strings.TrimSpace(primaryEntry.Text)),
				func(confirmed bool) {
					if !confirmed {
						return
					}
					startCommandExecution(args, taskName)
				},
				w,
			)
			return
		}

		startCommandExecution(args, taskName)
	})
	actionBtn.Importance = widget.LowImportance

	btnBg = canvas.NewRectangle(darkBlue)
	btnBg.CornerRadius = 6

	btnIcon = canvas.NewImageFromResource(dlActionIconRes)
	btnIcon.FillMode = canvas.ImageFillContain
	btnIcon.SetMinSize(fyne.NewSize(16, 16))

	btnLabel = canvas.NewText("Start Download", textEnabledColor)
	btnLabel.TextStyle = fyne.TextStyle{Bold: true}
	btnLabel.Alignment = fyne.TextAlignCenter

	labelCentered := container.NewPadded(
		container.NewHBox(
			layout.NewSpacer(),
			btnIcon,
			btnLabel,
			layout.NewSpacer(),
		),
	)

	hoverOverlay := newHoverBox(actionBtn, func(hovered bool) {
		isHovered = hovered
		currentText := getActionLabel(commandSelect.Selected, false)
		if btnLabel != nil && btnLabel.Text != "" {
			currentText = btnLabel.Text
		}
		fyne.Do(func() {
			applyBtnColor(currentText, !actionBtn.Disabled())
		})
	})
	actionView = container.NewStack(btnBg, labelCentered, actionBtn, hoverOverlay)

	// --------------------------------------------------------
	// Settings & Accounts Dialog
	// --------------------------------------------------------

	refreshAccountStatus := func(showPopup bool) {
		go func() {
			exePath, err := tgdlp.ResolvePath()
			if err != nil {
				fyne.Do(func() {
					statusLabel.SetText("tg-dlp not found")
					if showPopup {
						showTgDlpNotFoundDialog(w)
					}
				})
				return
			}
			exePathGlobal = exePath

			outStr, loggedIn := tgdlp.CheckWhoAmI(exePath, strings.TrimSpace(proxyEntry.Text), noProxyCheck.Checked)
			mu.Lock()
			hasLogin = loggedIn
			running := cancel != nil
			mu.Unlock()

			fyne.Do(func() {
				setActionState(getActionLabel(commandSelect.Selected, running), loggedIn || running)
				if outStr != "" {
					appendLogAndStats("[whoami] " + outStr)
				}
				if loggedIn {
					statusLabel.SetText("Ready (Authenticated)")
					if showPopup {
						dialog.ShowInformation("Account Active", "Logged in:\n"+outStr, w)
					}
				} else {
					statusLabel.SetText("No Telegram account. Settings > Manage Accounts > Add Account")
					if showPopup {
						dialog.ShowInformation("Not Logged In", "No active Telegram account found.\nOpen Settings > Manage Accounts and choose Add Account.", w)
					}
				}
			})
		}()
	}

	openSettingsAndAccountsDialog = func() {
		// ---- Appearance ----
		themeSel := widget.NewSelect([]string{"Dark", "Light"}, func(v string) {
			applyTheme(v == "Dark") // saved with the rest of the config
		})
		if isDarkTheme {
			themeSel.SetSelected("Dark")
		} else {
			themeSel.SetSelected("Light")
		}

		// ---- Accounts ----
		accountsBtn := widget.NewButtonWithIcon(
			"Manage Accounts",
			svgIconResource("set_accounts.svg", accountsSVG, isDarkTheme),
			func() {
				showAccountsDialog(
					w,
					func() []string { // proxy flags for the QR login
						if p := strings.TrimSpace(proxyEntry.Text); p != "" {
							return []string{"--proxy", p}
						}
						if noProxyCheck.Checked {
							return []string{"--no-proxy"}
						}
						return nil
					},
					func() { refreshAccountStatus(false) }, // account changed -> re-check whoami
				)
			},
		)

		whoamiBtn := widget.NewButtonWithIcon(
			"WhoAmI",
			svgIconResource("set_whoami.svg", whoamiSVG, isDarkTheme),
			func() {
				refreshAccountStatus(true)
			},
		)

		// ---- Current account ----
		accountNote := widget.NewLabel("These two apply to the current account (choose it in Manage Accounts).")
		accountNote.Wrapping = fyne.TextWrapWord

		clearCacheBtn := widget.NewButtonWithIcon(
			"Clear Cache",
			svgIconResource("set_clear_cache.svg", clearCacheSVG, isDarkTheme),
			func() {
				dialog.ShowConfirm("Clear Session Cache", "Clear ONLY the cached session password?", func(ok bool) {
					if ok {
						startCommandExecution([]string{"clear-cache"}, "Clear Cache")
					}
				}, w)
			},
		)

		logoutBtn := widget.NewButtonWithIcon(
			"Logout",
			svgIconResource("set_logout.svg", logoutSVG, !isDarkTheme), // Danger button background ပေါ်မှာ အမြဲအဖြူရောင်ပေါ်အောင် true ပေးထားပါတယ်
			func() {
				dialog.ShowConfirm("Logout", "Clear the cached session password and logout?", func(ok bool) {
					if ok {
						startCommandExecution([]string{"logout"}, "Logout")
					}
				}, w)
			},
		)
		logoutBtn.Importance = widget.DangerImportance

		// ---- Folders ----
		showFolderErr := func(err error) {
			if err != nil {
				dialog.ShowError(err, w)
			}
		}
		openDataBtn := widget.NewButtonWithIcon("tg-dlp data folder", theme.FolderOpenIcon(), func() {
			cfg, err := tgdlp.ConfigDir()
			if err != nil {
				showFolderErr(err)
				return
			}
			showFolderErr(openFolder(cfg))
		})
		openLogsBtn := widget.NewButtonWithIcon("GUI logs folder", theme.FolderOpenIcon(), func() {
			showFolderErr(openFolder(persist.LogsDir()))
		})

		// ---- tg-dlp ----
		pathText := "tg-dlp not found"
		if p, err := tgdlp.ResolvePath(); err == nil {
			pathText = p
		}
		pathLbl := widget.NewLabel("Using: " + pathText)
		pathLbl.Wrapping = fyne.TextWrapBreak

		versionBtn := widget.NewButtonWithIcon(
			"tg-dlp Version",
			svgIconResource("set_version.svg", versionSVG, isDarkTheme),
			func() {
				runCommandWithPopup(
					w,
					"tg-dlp Version",
					[]string{"version"},
					strings.TrimSpace(proxyEntry.Text),
					noProxyCheck.Checked,
					appendLogAndStats,
				)
			},
		)

		updateBtn := widget.NewButtonWithIcon(
			"tg-dlp Update",
			svgIconResource("set_update.svg", updateSVG, isDarkTheme),
			func() {
				runCommandWithPopup(
					w,
					"tg-dlp Update",
					[]string{"update"},
					strings.TrimSpace(proxyEntry.Text),
					noProxyCheck.Checked,
					appendLogAndStats,
				)
			},
		)

		bold := func(s string) *widget.Label {
			return widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		}
		content := container.NewVBox(
			bold("Appearance"),
			container.NewGridWithColumns(2, widget.NewLabel("Theme"), themeSel),
			widget.NewSeparator(),
			bold("Authentication & Accounts"),
			container.NewGridWithColumns(2, accountsBtn, whoamiBtn),
			widget.NewSeparator(),
			bold("Current account"),
			accountNote,
			container.NewGridWithColumns(2, clearCacheBtn, logoutBtn),
			widget.NewSeparator(),
			bold("Folders"),
			container.NewGridWithColumns(2, openDataBtn, openLogsBtn),
			widget.NewSeparator(),
			bold("tg-dlp"),
			pathLbl,
			container.NewGridWithColumns(2, versionBtn, updateBtn),
		)

		d := dialog.NewCustom("tg-dlp Settings & Accounts", "Close", container.NewVScroll(content), w)
		d.Resize(fyne.NewSize(620, 600))
		d.Show()
	}

	// --------------------------------------------------------
	// Layout Construction
	// --------------------------------------------------------

	commandSelectBox := container.NewGridWrap(
		fyne.NewSize(175, commandSelect.MinSize().Height),
		commandSelect,
	)

	topActionsBar := container.NewHBox(
		widget.NewLabelWithStyle("Command:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		commandSelectBox,
		layout.NewSpacer(),
		topRightActions,
	)

	topGrid := container.NewGridWithColumns(
		2,
		container.NewVBox(
			primaryFieldLabel,
			makeClearableEntry(primaryEntry),
		),
		container.NewVBox(
			secondaryFieldLabel,
			container.NewBorder(nil, nil, nil, browseOutBtn, makeClearableEntry(secondaryEntry)),
		),
		container.NewVBox(
			tertiaryLabel,
			makeClearableEntry(tertiaryEntry),
		),
		container.NewVBox(
			container.New(&equalHBoxLayout{gap: theme.Padding()}, quaternaryLabel, templateLabel),
			container.New(&equalHBoxLayout{gap: theme.Padding()}, makeClearableEntry(quaternaryEntry), templateBox),
		),
		container.NewVBox(
			concurrencyLabel,
			makeClearableEntry(concurrencyEntry),
		),
		container.NewVBox(
			widget.NewLabel("Proxy (--proxy):"),
			makeClearableEntry(proxyEntry),
		),
		container.NewVBox(
			widget.NewLabel("Extra Args:"),
			makeClearableEntry(customArgEntry),
		),
		container.NewVBox(
			layout.NewSpacer(),
			container.NewHBox(
				layout.NewSpacer(),
				container.NewVBox(widget.NewLabel("After task actions:"), afterSelect),
				container.NewVBox(layout.NewSpacer(), actionView),
			),
			layout.NewSpacer(),
		),
	)

	globalFlagsRow := container.NewHBox(
		widget.NewLabel("Global:"),
		noProxyCheck,
		skipFloodCheck,
		debugCheck,
	)

	commandOptionsStack := container.NewVBox(
		dlOptsRow,
		upOptsRow,
		infoOptsRow,
		purgeOptsRow,
		globalFlagsRow,
	)

	statsRow := container.NewHBox(
		pctLabel,
		widget.NewSeparator(),
		speedLabel,
		widget.NewSeparator(),
		etaLabel,
		widget.NewSeparator(),
		sizeLabel,
		widget.NewSeparator(),
		fileLabel,
	)

	progressSection := container.NewVBox(
		statsRow,
		progressBar,
		infiniteBar,
	)

	form := newCompactVBox(
		4,
		topActionsBar,
		topGrid,
		commandOptionsStack,
		progressSection,
	)

	historyBtn := widget.NewButtonWithIcon("History", theme.HistoryIcon(), func() {
		showHistoryWindow(a, historyDB, loadFromHistory)
	})
	historyBtn.Importance = widget.LowImportance

	statusBar := container.NewVBox(
		widget.NewSeparator(),
		container.NewBorder(nil, nil, statusLabel, container.NewHBox(historyBtn, clearLogBtn)),
	)

	w.SetContent(
		container.NewBorder(
			form,
			statusBar,
			nil,
			nil,
			logBox,
		),
	)

	// Initialize UI mode & disabled action button until account check completes
	syncCommandUI("Download (dl)")

	// Restore the last used configuration
	if haveCfg {
		proxyEntry.SetText(cfg.Proxy)
		noProxyCheck.SetChecked(cfg.Global["noProxy"])
		skipFloodCheck.SetChecked(cfg.Global["skipFlood"])
		debugCheck.SetChecked(cfg.Global["debug"])

		if p, ok := profiles["Download (dl)"]; ok {
			applyProfile(p) // we are on the default command right now
		}
		for _, opt := range commandSelect.Options {
			if opt == cfg.Command && opt != commandSelect.Selected {
				commandSelect.SetSelected(opt) // OnChanged applies that command's profile
				break
			}
		}
		if cfg.Theme == "light" {
			applyTheme(false)
		}
	}
	setActionState(getActionLabel(commandSelect.Selected, false), false)

	// values used by the startup account check (a saved proxy may be required to reach Telegram)
	startProxy := strings.TrimSpace(proxyEntry.Text)
	startNoProxy := noProxyCheck.Checked

	// --------------------------------------------------------
	// Auto Check Account on Startup
	// --------------------------------------------------------

	go func() {
		time.Sleep(400 * time.Millisecond)

		exePath, err := tgdlp.ResolvePath()
		if err != nil {
			fyne.Do(func() {
				statusLabel.SetText("tg-dlp not found")
				showTgDlpNotFoundDialog(w)
			})
			return
		}
		exePathGlobal = exePath

		outStr, loggedIn := tgdlp.CheckWhoAmI(exePath, startProxy, startNoProxy)
		mu.Lock()
		hasLogin = loggedIn
		mu.Unlock()

		fyne.Do(func() {
			if outStr != "" {
				appendLogAndStats(outStr)
			}
			if !loggedIn {
				statusLabel.SetText("No Telegram account. Settings > Manage Accounts > Add Account")
				setActionState(getActionLabel(commandSelect.Selected, false), false)
				openSettingsAndAccountsDialog()
			} else {
				statusLabel.SetText("Ready")
				setActionState(getActionLabel(commandSelect.Selected, false), true)
			}
		})
	}()

	// --------------------------------------------------------
	// Window Close Handler
	// --------------------------------------------------------

	// Auto-save the configuration whenever it changed (checked every 2s)
	go func() {
		last := ""
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(func() {
				c := snapshotConfig()
				b, err := json.Marshal(c)
				if err != nil || string(b) == last {
					return
				}
				if persist.SaveConfig(c) == nil {
					last = string(b)
				}
			})
		}
	}()

	quitApp = func() {
		_ = persist.SaveConfig(snapshotConfig())
		mu.Lock()
		if cancel != nil {
			cancel()
		}
		mu.Unlock()
		if historyWin != nil {
			historyWin.Close()
		}
		w.Close()
	}
	w.SetCloseIntercept(quitApp)

	w.CenterOnScreen()
	w.ShowAndRun()
}
