package progress

import (
	"regexp"
	"strconv"
	"strings"
)

type Progress struct {
	HasPercent bool
	Percent    float64
	PctText    string

	SizeText  string
	SpeedText string
	ETAText   string

	FileName string
}

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	// OSC sequences (e.g. window title: ESC ] 0 ; title BEL)
	oscRegex  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	ctrlRegex = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	pctRegex  = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)

	rateRegex  = regexp.MustCompile(`([\d.]+)\s*([KMGT]?i?B)/s`)
	FilesRegex = regexp.MustCompile(`(\d+)\s*/\s*(\d+)\s*files`)
)

func ParseLine(line string) Progress {
	var p Progress

	trimmed := strings.TrimSpace(StripANSI(line))
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
