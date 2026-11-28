package tgdlp

import (
	"strings"
)

func SplitCustomArgs(input string) []string {
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

// ProxyFromArgs turns the global proxy flags the dialog receives
// ("--proxy <url>" / "--no-proxy") into the values the login needs.
func ProxyFromArgs(args []string) (proxy string, noProxy bool) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--proxy":
			if i+1 < len(args) {
				proxy = args[i+1]
				i++
			}
		case "--no-proxy":
			noProxy = true
		}
	}
	return proxy, noProxy
}
