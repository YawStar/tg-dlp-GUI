package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	"tg-dlp_GUI/internal/platform"
	"tg-dlp_GUI/internal/progress"
	"tg-dlp_GUI/internal/tgdlp"
)

func runCommandWithPopup(
	w fyne.Window,
	title string,
	args []string,
	proxyURL string,
	noProxy bool,
	appendLog func(string),
) {
	go func() {
		exePath, err := tgdlp.ResolvePath()
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
		platform.HideConsole(cmd)
		output, err := cmd.CombinedOutput()
		outStr := strings.TrimSpace(progress.StripANSI(string(output)))

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

func runInteractiveInTerminal(
	w fyne.Window,
	args []string,
	onFinished func(),
) {
	go func() {
		exePath, err := tgdlp.ResolvePath()
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

		if err := platform.RunInTerminal(exePath, args); err != nil {
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
