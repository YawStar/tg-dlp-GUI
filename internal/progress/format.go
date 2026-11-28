// Package progress parses the (ANSI / CR-LF) output of tg-dlp into progress
// information and formats sizes, speeds and durations.

package progress

import (
	"fmt"
	"strconv"
)

func ParseRateBytes(s string) float64 {
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

func FormatRate(b float64) string {
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

func FormatDuration(secs int) string {
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

func StripANSI(s string) string {
	s = oscRegex.ReplaceAllString(s, "")
	s = ansiRegex.ReplaceAllString(s, "")
	return ctrlRegex.ReplaceAllString(s, "")
}
