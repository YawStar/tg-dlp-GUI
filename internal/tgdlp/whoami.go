package tgdlp

import (
	"os/exec"
	"strings"

	"tg-dlp_GUI/internal/platform"
	"tg-dlp_GUI/internal/progress"
)

func CheckWhoAmI(exePath string, proxyURL string, noProxy bool) (string, bool) {
	var args []string
	if proxyURL != "" {
		args = append(args, "--proxy", proxyURL)
	} else if noProxy {
		args = append(args, "--no-proxy")
	}
	args = append(args, "whoami")

	cmd := exec.Command(exePath, args...)
	platform.HideConsole(cmd) // no flashing console window on Windows
	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(progress.StripANSI(string(output)))
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
