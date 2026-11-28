// Package platform holds everything that differs per operating system:
// pseudo terminals (ConPTY on Windows, creack/pty elsewhere), opening a
// visible terminal window and the Windows taskbar progress indicator.

package platform

import (
	"strings"
)

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

func BuildCommandLine(exePath string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteWindowsArg(exePath))
	for _, arg := range args {
		parts = append(parts, quoteWindowsArg(arg))
	}
	return strings.Join(parts, " ")
}
