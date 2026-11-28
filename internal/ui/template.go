package ui

import (
	"fmt"
	"strings"
)

const invalidFilenameChars = `\/:*?"<>|`

// validateFilenameTemplate checks a template for "tg-dlp dl -t". Any
// placeholder is allowed as-is, and templates without placeholders are
// allowed; it only rejects characters that cannot be used in filenames.
func validateFilenameTemplate(t string) error {
	if strings.ContainsAny(t, invalidFilenameChars) {
		return fmt.Errorf("Template contains invalid filename characters (%s).", invalidFilenameChars)
	}
	return nil
}

// splitFilterTemplate returns the filter (-i) and template (-t) to use.
// Older versions of the GUI kept both in one field and told them apart by a
// "{": a saved value like "{id}_{name}" in the filter field is treated as the
// template, so old settings still work.
func splitFilterTemplate(filter, tmpl string) (string, string) {
	if tmpl == "" && strings.Contains(filter, "{") {
		return "", filter
	}
	return filter, tmpl
}
