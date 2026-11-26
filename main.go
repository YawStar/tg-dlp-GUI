package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"image/color"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ============================================================
// Global Variables
// ============================================================

var exePathGlobal string

// ptySession is a running tg-dlp process attached to a pseudo terminal.
// Implemented per-OS in platform_windows.go (ConPTY) and platform_unix.go (creack/pty).
type ptySession interface {
	io.Reader
	Write(p []byte) (int, error)
	Wait() (int, error) // blocks until the process exits, returns exit code
	Interrupt()         // graceful stop (Ctrl+C / SIGINT)
	Kill()              // force kill (whole process tree)
	Close() error
}

// ============================================================
// Progress Structure
// ============================================================

type dlProgress struct {
	HasPercent bool
	Percent    float64
	PctText    string

	SizeText  string
	SpeedText string
	ETAText   string

	FileName string
}

type pendEntry struct {
	key  string
	text string
}

// ============================================================
// Regular Expressions
// ============================================================

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	// OSC sequences (e.g. window title: ESC ] 0 ; title BEL)
	oscRegex  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	ctrlRegex = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	pctRegex  = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)

	rateRegex  = regexp.MustCompile(`([\d.]+)\s*([KMGT]?i?B)/s`)
	filesRegex = regexp.MustCompile(`(\d+)\s*/\s*(\d+)\s*files`)
)

// ============================================================
// Custom SVG Icons
// ============================================================

const (
	sunSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/></svg>`

	moonSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>`

	githubSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/></svg>`

	infoSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><title>info</title><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`

	downloadSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 15V3"/><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/></svg>`

	stopSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M4.929 4.929 19.07 19.071"/></svg>`

	settingsSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="lucide lucide-settings preview-icon"><path d="M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915"/><circle cx="12" cy="12" r="3"/></svg>`

	heartSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 9.5a5.5 5.5 0 0 1 9.591-3.676.56.56 0 0 0 .818 0A5.49 5.49 0 0 1 22 9.5c0 2.29-1.5 4-3 5.5l-5.492 5.313a2 2 0 0 1-3 .019L5 15c-1.5-1.5-3-3.2-3-5.5"/></svg>`

	// ---- action button icons (Lucide), one per command ----
	// (infoSVG above is the top-bar icon, so the Info command uses actInfoSVG)
	actUploadSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12"/><path d="m17 8-5-5-5 5"/><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/></svg>`

	actExportSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 12h10"/><path d="m17 16 4-4-4-4"/><path d="M21 6.33V5a2 2 0 00-2-2H5a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-1.33"/></svg>`

	actBackupSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h.343"/><path d="M20 8v11a2 2 0 0 1-2 2h-.343"/><path d="m9 18 3 3 3-3"/><path d="M12 12v9"/></svg>`

	actRestoreSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h2"/><path d="M20 8v11a2 2 0 0 1-2 2h-2"/><path d="m9 15 3-3 3 3"/><path d="M12 12v9"/></svg>`

	actInfoSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 1-4.78-4.77 4 4 0 0 1 0-6.76Z"/><line x1="12" x2="12" y1="16" y2="12"/><line x1="12" x2="12.01" y1="8" y2="8"/></svg>`

	actDeleteSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/></svg>`

	actPurgeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m16 22-1-4"/><path d="M19 14a1 1 0 0 0 1-1v-1a2 2 0 0 0-2-2h-3a1 1 0 0 1-1-1V4a2 2 0 0 0-4 0v5a1 1 0 0 1-1 1H6a2 2 0 0 0-2 2v1a1 1 0 0 0 1 1"/><path d="M19 14H5l-1.973 6.767A1 1 0 0 0 4 22h16a1 1 0 0 0 .973-1.233z"/><path d="m8 22 1-4"/></svg>`
)

type iconSet struct {
	sun      fyne.Resource
	moon     fyne.Resource
	github   fyne.Resource
	info     fyne.Resource
	settings fyne.Resource
	heart    fyne.Resource
}

func svgIconResource(name string, svgXML string, dark bool) fyne.Resource {
	colorHex := "#111827"
	if dark {
		colorHex = "#FFFFFF"
	}
	themedSVG := strings.ReplaceAll(strings.TrimSpace(svgXML), "currentColor", colorHex)
	return fyne.NewStaticResource(name+"_"+colorHex[1:], []byte(themedSVG))
}

func buildIconSet(dark bool) iconSet {
	return iconSet{
		sun:      svgIconResource("sun.svg", sunSVG, dark),
		moon:     svgIconResource("moon.svg", moonSVG, dark),
		github:   svgIconResource("github.svg", githubSVG, dark),
		info:     svgIconResource("info.svg", infoSVG, dark),
		settings: svgIconResource("settings.svg", settingsSVG, dark),
		heart:    svgIconResource("heart.svg", heartSVG, dark),
	}
}

// ============================================================
// Utility Functions
// ============================================================

func parseRateBytes(s string) float64 {
	m := rateRegex.FindStringSubmatch(s)
	if len(m) < 3 {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}
	return v * mult[m[2]]
}

func formatRate(b float64) string {
	units := []string{"B/s", "KiB/s", "MiB/s", "GiB/s"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return fmt.Sprintf("%.2f %s", b, units[i])
}

func formatDuration(secs int) string {
	if secs < 0 {
		secs = 0
	}
	h, m, sec := secs/3600, (secs%3600)/60, secs%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, sec)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

func stripANSI(s string) string {
	s = oscRegex.ReplaceAllString(s, "")
	s = ansiRegex.ReplaceAllString(s, "")
	return ctrlRegex.ReplaceAllString(s, "")
}

func splitCustomArgs(input string) []string {
	var args []string
	var current strings.Builder
	var quoteChar rune
	escaped := false

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		if quoteChar != 0 {
			if r == quoteChar {
				quoteChar = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}

		if r == '"' || r == '\'' {
			quoteChar = r
			continue
		}

		if r == ' ' || r == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// ============================================================
// Progress Parser
// ============================================================

func parseProgressLine(line string) dlProgress {
	var p dlProgress

	trimmed := strings.TrimSpace(stripANSI(line))
	if trimmed == "" {
		return p
	}

	if m := pctRegex.FindStringSubmatch(trimmed); len(m) >= 2 {
		val, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			if val < 0 {
				val = 0
			}
			if val > 100 {
				val = 100
			}
			p.HasPercent = true
			p.Percent = val / 100.0
			p.PctText = m[1] + "%"
		}
	}

	parts := strings.Split(trimmed, "|")
	if len(parts) >= 2 {
		p.SizeText = strings.TrimSpace(parts[1])
	}
	if len(parts) >= 3 {
		p.SpeedText = strings.TrimSpace(parts[2])
	}
	if len(parts) >= 4 {
		etaRaw := strings.TrimSpace(parts[3])
		etaRaw = strings.TrimSpace(strings.TrimPrefix(etaRaw, "ETA:"))
		etaRaw = strings.TrimSpace(strings.TrimPrefix(etaRaw, "ETA"))
		p.ETAText = etaRaw
	}

	if strings.HasPrefix(trimmed, "Total") {
		if len(parts) >= 2 {
			p.SizeText = strings.TrimSpace(parts[1])
		}
		p.SpeedText = ""
		p.ETAText = ""
		return p
	}

	if strings.HasPrefix(trimmed, "File:") {
		p.FileName = strings.TrimSpace(strings.TrimPrefix(parts[0], "File:"))
		if len(parts) >= 2 {
			p.SizeText = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[1]), "Size:"))
		}
	} else if idx := strings.Index(trimmed, "Downloaded:"); idx != -1 {
		after := strings.TrimSpace(trimmed[idx+len("Downloaded:"):])
		if cutIdx := strings.Index(after, " (in "); cutIdx != -1 {
			after = strings.TrimSpace(after[:cutIdx])
		}
		if after != "" {
			p.FileName = after
		}
	} else if idx := strings.Index(trimmed, "Downloading:"); idx != -1 {
		after := strings.TrimSpace(trimmed[idx+len("Downloading:"):])
		if after != "" {
			p.FileName = after
		}
	}

	return p
}

// ============================================================
// CR / LF Scanner
// ============================================================

func scanCRorLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[:i], nil
		}
	}

	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}

// ============================================================
// Windows Command Line Quoting & Builder
// ============================================================

func quoteWindowsArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}

	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0

	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
		case '"':
			for i := 0; i < backslashes*2+1; i++ {
				b.WriteByte('\\')
			}
			b.WriteRune('"')
			backslashes = 0
		default:
			for i := 0; i < backslashes; i++ {
				b.WriteByte('\\')
			}
			backslashes = 0
			b.WriteRune(r)
		}
	}

	for i := 0; i < backslashes*2; i++ {
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}

func buildCommandLine(exePath string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteWindowsArg(exePath))
	for _, arg := range args {
		parts = append(parts, quoteWindowsArg(arg))
	}
	return strings.Join(parts, " ")
}

// ============================================================
// Resolve tg-dlp Executable
// ============================================================

func tgDlpNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"tg-dlp.exe", "tg-dlp"}
	}
	return []string{"tg-dlp"}
}

func resolveTgDlpPath() (string, error) {
	names := tgDlpNames()
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return filepath.Abs(p)
		}
	}

	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	for _, d := range dirs {
		for _, n := range names {
			c := filepath.Join(d, n)
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c, nil
			}
		}
	}

	return "", fmt.Errorf("tg-dlp not found in PATH or application directory")
}

// ============================================================
// Account Status Checker (whoami)
// ============================================================

func checkWhoAmI(exePath string, proxyURL string, noProxy bool) (string, bool) {
	var args []string
	if proxyURL != "" {
		args = append(args, "--proxy", proxyURL)
	} else if noProxy {
		args = append(args, "--no-proxy")
	}
	args = append(args, "whoami")

	cmd := exec.Command(exePath, args...)
	hideConsole(cmd) // no flashing console window on Windows
	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(stripANSI(string(output)))
	lowerOut := strings.ToLower(outStr)

	if outStr == "" || err != nil {
		return outStr, false
	}

	noAccountKeywords := []string{
		"no account",
		"not logged in",
		"no session",
		"please login",
		"unauthorized",
		"failed to load",
		"not authenticated",
	}
	for _, kw := range noAccountKeywords {
		if strings.Contains(lowerOut, kw) {
			return outStr, false
		}
	}

	return outStr, true
}

func runCommandWithPopup(
	w fyne.Window,
	title string,
	args []string,
	proxyURL string,
	noProxy bool,
	appendLog func(string),
) {
	go func() {
		exePath, err := resolveTgDlpPath()
		if err != nil {
			fyne.Do(func() {
				showTgDlpNotFoundDialog(w)
			})
			return
		}
		exePathGlobal = exePath

		var fullArgs []string
		if proxyURL != "" {
			fullArgs = append(fullArgs, "--proxy", proxyURL)
		} else if noProxy {
			fullArgs = append(fullArgs, "--no-proxy")
		}
		fullArgs = append(fullArgs, args...)

		cmd := exec.Command(exePath, fullArgs...)
		hideConsole(cmd)
		output, err := cmd.CombinedOutput()
		outStr := strings.TrimSpace(stripANSI(string(output)))

		fyne.Do(func() {
			if outStr != "" && appendLog != nil {
				appendLog(outStr)
			}
			if err != nil && outStr == "" {
				dialog.ShowError(fmt.Errorf("Failed to run %s:\n%v", title, err), w)
				return
			}
			if outStr == "" {
				outStr = "No output returned."
			}
			dialog.ShowInformation(title, outStr, w)
		})
	}()
}

// ============================================================
// Helper UI Builders
// ============================================================

func makeClearableEntry(entry *widget.Entry) fyne.CanvasObject {
	var clearBtn *widget.Button
	clearBtn = widget.NewButton("×", func() {
		entry.SetText("")
		entry.FocusGained()
	})
	clearBtn.Importance = widget.LowImportance

	updateClearVisibility := func(s string) {
		if strings.TrimSpace(s) == "" {
			clearBtn.Hide()
		} else {
			clearBtn.Show()
		}
	}

	entry.OnChanged = func(s string) {
		updateClearVisibility(s)
	}
	updateClearVisibility(entry.Text)

	rightOverlay := container.NewBorder(nil, nil, nil, clearBtn, layout.NewSpacer())
	return container.NewStack(entry, rightOverlay)
}

type compactVBoxLayout struct {
	gap float32
}

func (c *compactVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	visibleCount := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		h += m.Height
		visibleCount++
	}
	if visibleCount > 1 {
		h += c.gap * float32(visibleCount-1)
	}
	return fyne.NewSize(w, h)
}

func (c *compactVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	first := true
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		if !first {
			y += c.gap
		}
		first = false
		m := o.MinSize()
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, m.Height))
		y += m.Height
	}
}

func newCompactVBox(gap float32, objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(&compactVBoxLayout{gap: gap}, objects...)
}

type hoverBox struct {
	widget.BaseWidget
	btn      *widget.Button
	onTap    func()
	onChange func(hovered bool)
	hovered  bool
}

func newHoverBox(btn *widget.Button, onChange func(hovered bool)) *hoverBox {
	h := &hoverBox{
		btn:      btn,
		onChange: onChange,
	}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hoverBox) CreateRenderer() fyne.WidgetRenderer {
	rect := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(rect)
}

func (h *hoverBox) Cursor() desktop.Cursor {
	if h.hovered && (h.btn == nil || !h.btn.Disabled()) {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (h *hoverBox) MouseIn(_ *desktop.MouseEvent) {
	h.hovered = true
	if h.onChange != nil {
		h.onChange(true)
	}
}

func (h *hoverBox) MouseMoved(_ *desktop.MouseEvent) {
	if !h.hovered {
		h.hovered = true
		if h.onChange != nil {
			h.onChange(true)
		}
	}
}

func (h *hoverBox) MouseOut() {
	h.hovered = false
	if h.onChange != nil {
		h.onChange(false)
	}
}

func (h *hoverBox) Tapped(ev *fyne.PointEvent) {
	if h.btn != nil && !h.btn.Disabled() {
		h.btn.Tapped(ev)
		return
	}
	if h.onTap != nil {
		h.onTap()
	}
}

// ============================================================
// Terminal Interactive Runner (Login / Accounts Management)
// ============================================================

func runInteractiveInTerminal(
	w fyne.Window,
	args []string,
	onFinished func(),
) {
	go func() {
		exePath, err := resolveTgDlpPath()
		if err != nil {
			fyne.Do(func() {
				showTgDlpNotFoundDialog(w)
				if onFinished != nil {
					onFinished()
				}
			})
			return
		}
		exePathGlobal = exePath

		if err := runInTerminal(exePath, args); err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("Failed to open terminal:\n%v", err), w)
				if onFinished != nil {
					onFinished()
				}
			})
			return
		}

		time.Sleep(500 * time.Millisecond)

		if onFinished != nil {
			fyne.Do(onFinished)
		}
	}()
}

//go:embed assets/app-icon.png
var appIconBytes []byte

// ============================================================
// "tg-dlp not found" dialog (download link + Windows install command)
// ============================================================

const (
	tgDlpReleasesURL = "https://github.com/YawStar/tg-dlp/releases/latest"
	tgDlpInstallPS   = "irm https://raw.githubusercontent.com/YawStar/tg-dlp/main/install.ps1 | iex"
)

var notFoundDialogOpen atomic.Bool

// showTgDlpNotFoundDialog tells the user tg-dlp is missing and how to get it.
// Must be called on the UI thread (inside fyne.Do when called from a goroutine).
func showTgDlpNotFoundDialog(w fyne.Window) {
	// Several startup checks can fail at once; show only one dialog.
	if !notFoundDialogOpen.CompareAndSwap(false, true) {
		return
	}

	msg := widget.NewLabel("tg-dlp was not found in PATH or in the application folder.\nInstall it, then restart this app.")
	msg.Wrapping = fyne.TextWrapWord

	items := []fyne.CanvasObject{msg}

	if u, err := url.Parse(tgDlpReleasesURL); err == nil {
		items = append(items, widget.NewHyperlink("Download tg-dlp (GitHub Releases)", u))
	}

	if runtime.GOOS == "windows" {
		psLbl := widget.NewLabel("Or run this in PowerShell:")
		psEntry := widget.NewEntry()
		psEntry.SetText(tgDlpInstallPS)
		// keep it selectable/copyable but not editable
		psEntry.OnChanged = func(t string) {
			if t != tgDlpInstallPS {
				psEntry.SetText(tgDlpInstallPS)
			}
		}
		copyBtn := widget.NewButtonWithIcon("Copy command", theme.ContentCopyIcon(), func() {
			w.Clipboard().SetContent(tgDlpInstallPS)
		})
		items = append(items, psLbl, psEntry, copyBtn)
	}

	d := dialog.NewCustom("tg-dlp not found", "OK", container.NewVBox(items...), w)
	d.SetOnClosed(func() { notFoundDialogOpen.Store(false) })
	d.Resize(fyne.NewSize(600, 260))
	d.Show()
}

// ============================================================
// Main
// ============================================================

func main() {
	a := app.New()
	w := a.NewWindow("tg-dlp GUI (Fyne)")

	// Last used configuration + task history (stored in the user's config dir)
	cfg, haveCfg := loadConfig()
	historyDB := openHistory()

	if cfg.WinW >= 600 && cfg.WinH >= 450 {
		w.Resize(fyne.NewSize(cfg.WinW, cfg.WinH))
	} else {
		w.Resize(fyne.NewSize(820, 650))
	}

	if len(appIconBytes) > 0 {
		w.SetIcon(fyne.NewStaticResource("app-icon.png", appIconBytes))
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
		pendTotal  *dlProgress
		pendItem   dlProgress
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

	clearLogBtn := widget.NewButton("Clear Log", func() {
		logMu.Lock()
		logBox.SetText("")
		logBox.CursorRow = 0
		logLines = nil
		logMu.Unlock()
	})
	clearLogBtn.Importance = widget.LowImportance

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
			clearTaskbarProgress(w)
		})
	}

	updateProgress := func(prog dlProgress, isTotal bool) {
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
				setTaskbarProgress(w, prog.Percent)
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
		clean := strings.TrimSpace(stripANSI(line))
		if clean == "" {
			return
		}
		prog := parseProgressLine(clean)
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
			histSummary.Store(prog.SizeText)
		}
		if !prog.HasPercent {
			taskLog.Load().Line(redactURLPassword(clean))
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
			if m := filesRegex.FindStringSubmatch(prog.SizeText); len(m) == 3 {
				done, _ := strconv.Atoi(m[1])
				total, _ := strconv.Atoi(m[2])
				now := time.Now()
				if firstTotalAt.IsZero() {
					firstTotalAt = now
					firstTotalDone = done
				} else if el := now.Sub(firstTotalAt).Seconds(); el >= 5 && done > firstTotalDone {
					rate := float64(done-firstTotalDone) / el
					aggETA = "~" + formatDuration(int(float64(total-done)/rate))
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
					activeSpeed[key] = parseRateBytes(prog.SpeedText)
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
			pendLog, pendTotal, pendItem, pendStatus, pendDirty = nil, nil, dlProgress{}, "", false
			pendIdx = nil
			aggSpeedText, aggETAText := "", ""
			if sawTotal.Load() {
				sum := 0.0
				for _, v := range activeSpeed {
					sum += v
				}
				aggSpeedText = formatRate(sum)
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
			quaternaryLabel.SetText("Filter / Template:")
			quaternaryEntry.SetPlaceHolder("Filter (-i, e.g. mp4,mkv) or Template (-t, e.g. {id}_{ext})")
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

	captureProfile := func() formProfile {
		p := formProfile{
			Primary:     primaryEntry.Text,
			Secondary:   secondaryEntry.Text,
			Tertiary:    tertiaryEntry.Text,
			Quaternary:  quaternaryEntry.Text,
			Concurrency: concurrencyEntry.Text,
			Extra:       customArgEntry.Text,
			Checks:      map[string]bool{},
		}
		for k, c := range profileChecks {
			p.Checks[k] = c.Checked
		}
		return p
	}

	applyProfile := func(p formProfile) {
		primaryEntry.SetText(p.Primary)
		secondaryEntry.SetText(p.Secondary)
		tertiaryEntry.SetText(p.Tertiary)
		quaternaryEntry.SetText(p.Quaternary)
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
			concurrencyEntry.SetText("")
		}
		curCmd = sel
	}

	loadFromHistory := func(e historyEntry) {
		if commandSelect.Selected != e.Command {
			commandSelect.SetSelected(e.Command)
		}
		if e.Form != nil {
			applyProfile(*e.Form)
		}
		statusLabel.SetText("Loaded settings from history")
		w.RequestFocus()
	}

	snapshotConfig := func() appConfig {
		profiles[curCmd] = captureProfile()
		c := appConfig{
			Command: commandSelect.Selected,
			Theme:   "dark",
			Proxy:   proxyEntry.Text,
			Global: map[string]bool{
				"noProxy":   noProxyCheck.Checked,
				"skipFlood": skipFloodCheck.Checked,
				"debug":     debugCheck.Checked,
			},
			Profiles: profiles,
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

		exePath, err := resolveTgDlpPath()
		fmt.Println("[cmd] " + buildCommandLine(exePath, redactArgs(args)))

		if err != nil {
			mu.Lock()
			cancel = nil
			mu.Unlock()
			c()
			appendLogAndStats("[!] Error: " + err.Error())
			fyne.Do(func() {
				statusLabel.SetText("tg-dlp not found")
				setTaskbarError(w)
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
		taskLog.Store(nil)
		hForm := captureProfile()
		hEntry := historyEntry{
			ID:      time.Now().Format("20060102-150405.000") + "-" + cmdShortName(taskTitle),
			Command: taskTitle,
			Target:  strings.TrimSpace(primaryEntry.Text),
			CmdLine: buildCommandLine("tg-dlp", redactArgs(args)),
			Started: time.Now(),
			Status:  "running",
			Form:    &hForm,
		}
		hEntry.LogFile = hEntry.ID + ".log"
		var hLog *taskLogger
		if recordHistory {
			historyDB.Add(hEntry)
			hLog = newTaskLogger(filepath.Join(logsDir(), hEntry.LogFile))
			taskLog.Store(hLog)
		}
		histSummary.Store("")
		var hOnce sync.Once
		finishHistory := func(status string, code int) {
			hOnce.Do(func() {
				taskLog.Store(nil)
				if !recordHistory {
					return
				}
				hLog.Close()
				e := hEntry
				e.Ended = time.Now()
				e.Status = status
				e.ExitCode = code
				e.Summary, _ = histSummary.Load().(string)
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
			setTaskbarIndeterminate(w)
			statusLabel.SetText("Starting " + taskTitle + "...")
		})

		go func() {
			appendLogAndStats("[cmd] " + buildCommandLine(exePath, redactArgs(args)))
			appendLogAndStats("[info] Starting pseudo terminal...")

			pty, err := startPTY(exePath, args)
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
					setTaskbarError(w)
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

			scanDone := make(chan struct{})
			go func() {
				defer close(scanDone)
				scanner := bufio.NewScanner(pty)
				scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
				scanner.Split(scanCRorLF)
				for scanner.Scan() {
					line := string(scanner.Bytes())
					appendLogAndStats(line)
					if captureInfo {
						if clean := strings.TrimSpace(stripANSI(line)); clean != "" {
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
					clearTaskbarProgress(w)
				})
				notifyDone("tg-dlp GUI", taskTitle+" cancelled.")
			} else if waitErr != nil {
				appendLogAndStats("[error] " + waitErr.Error())
				fyne.Do(func() {
					statusLabel.SetText(taskTitle + " failed")
					setTaskbarError(w)
				})
				notifyDone("tg-dlp GUI", taskTitle+" failed.")
			} else if exitCode != 0 {
				appendLogAndStats(fmt.Sprintf("[error] tg-dlp exited with code %d", exitCode))
				fyne.Do(func() {
					statusLabel.SetText(taskTitle + " failed")
					setTaskbarError(w)
				})
				notifyDone("tg-dlp GUI", taskTitle+" failed.")
			} else {
				fyne.Do(func() {
					barText = "100%"
					progressBar.SetValue(1.0)
					pctLabel.SetText("100.0%")
					etaLabel.SetText("ETA: 0s")
					statusLabel.SetText(taskTitle + " finished")
					clearTaskbarProgress(w)
				})
				appendLogAndStats("[done] All tasks completed!")
				notifyDone("tg-dlp GUI", taskTitle+" finished!")
			}

			hStatus := "finished"
			if ctx.Err() != nil {
				hStatus = "cancelled"
			} else if waitErr != nil || exitCode != 0 {
				hStatus = "failed"
			}
			finishHistory(hStatus, exitCode)

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
			if qVal != "" {
				if strings.Contains(qVal, "{") {
					args = append(args, "-t", qVal)
				} else {
					args = append(args, "-i", qVal)
				}
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
			args = append(args, splitCustomArgs(extraVal)...)
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
			exePath, err := resolveTgDlpPath()
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

			outStr, loggedIn := checkWhoAmI(exePath, strings.TrimSpace(proxyEntry.Text), noProxyCheck.Checked)
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
		pwEntry := widget.NewPasswordEntry()
		pwEntry.SetPlaceHolder("Session password")

		profileEntry := widget.NewEntry()
		profileEntry.SetPlaceHolder("Profile name (required)")

		cacheSecEntry := widget.NewEntry()
		cacheSecEntry.SetPlaceHolder("Cache seconds (e.g. 0)")
		cacheSecEntry.SetText("0")

		accountsBtn := widget.NewButton("Manage Accounts", func() {
			runInteractiveInTerminal(w, []string{"accounts"}, func() {
				refreshAccountStatus(false)
			})
		})

		whoamiBtn := widget.NewButton("WhoAmI", func() {
			refreshAccountStatus(true)
		})

		versionBtn := widget.NewButton("tg-dlp Version", func() {
			runCommandWithPopup(
				w,
				"tg-dlp Version",
				[]string{"version"},
				strings.TrimSpace(proxyEntry.Text),
				noProxyCheck.Checked,
				appendLogAndStats,
			)
		})

		updateBtn := widget.NewButton("tg-dlp Update", func() {
			runCommandWithPopup(
				w,
				"tg-dlp Update",
				[]string{"update"},
				strings.TrimSpace(proxyEntry.Text),
				noProxyCheck.Checked,
				appendLogAndStats,
			)
		})

		setPWBtn := widget.NewButton("Set Password (config-pw)", func() {
			pw := strings.TrimSpace(pwEntry.Text)
			if pw == "" {
				dialog.ShowError(fmt.Errorf("Please enter a session password."), w)
				return
			}
			prof := strings.TrimSpace(profileEntry.Text)
			if prof == "" {
				dialog.ShowError(fmt.Errorf("Please enter a profile name."), w)
				return
			}
			startCommandExecution([]string{"config-pw", pw, "--profile", prof}, "Config Password")
		})

		setCacheBtn := widget.NewButton("Set Cache (config-cache)", func() {
			sec := strings.TrimSpace(cacheSecEntry.Text)
			if _, err := strconv.Atoi(sec); err != nil {
				dialog.ShowError(fmt.Errorf("Cache value must be an integer (e.g. 0)."), w)
				return
			}
			startCommandExecution([]string{"config-cache", sec}, "Config Cache")
		})

		clearCacheBtn := widget.NewButton("Clear Cache", func() {
			dialog.ShowConfirm("Clear Session Cache", "Clear ONLY the cached session password?", func(ok bool) {
				if ok {
					startCommandExecution([]string{"clear-cache"}, "Clear Cache")
				}
			}, w)
		})

		logoutBtn := widget.NewButton("Logout", func() {
			dialog.ShowConfirm("Logout", "Clear the cached session password and logout?", func(ok bool) {
				if ok {
					startCommandExecution([]string{"logout"}, "Logout")
				}
			}, w)
		})
		logoutBtn.Importance = widget.DangerImportance

		content := container.NewVBox(
			widget.NewLabelWithStyle("Authentication & Accounts", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewGridWithColumns(2, accountsBtn, whoamiBtn),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Session Password & Cache", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewGridWithColumns(2, pwEntry, profileEntry),
			setPWBtn,
			container.NewGridWithColumns(2, cacheSecEntry, setCacheBtn),
			container.NewGridWithColumns(2, clearCacheBtn, logoutBtn),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("App Utilities", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewGridWithColumns(2, versionBtn, updateBtn),
		)

		d := dialog.NewCustom("tg-dlp Settings & Accounts", "Close", content, w)
		d.Resize(fyne.NewSize(620, 440))
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
			quaternaryLabel,
			makeClearableEntry(quaternaryEntry),
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
				actionView,
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

		exePath, err := resolveTgDlpPath()
		if err != nil {
			fyne.Do(func() {
				statusLabel.SetText("tg-dlp not found")
				showTgDlpNotFoundDialog(w)
			})
			return
		}
		exePathGlobal = exePath

		outStr, loggedIn := checkWhoAmI(exePath, startProxy, startNoProxy)
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
				if saveConfig(c) == nil {
					last = string(b)
				}
			})
		}
	}()

	w.SetCloseIntercept(func() {
		_ = saveConfig(snapshotConfig())
		mu.Lock()
		if cancel != nil {
			cancel()
		}
		mu.Unlock()
		if historyWin != nil {
			historyWin.Close()
		}
		w.Close()
	})

	w.CenterOnScreen()
	w.ShowAndRun()
}
