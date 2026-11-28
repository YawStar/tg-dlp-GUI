package tgauth

import (
	"fmt"
	"unicode/utf8"
)

const MinSessionPassword = 4

type CacheChoice struct {
	Label   string
	Seconds int // 0 = until the session file is deleted, CacheNever = do not remember
}

// "Remember the session password for ..." (same options as "tg-dlp config-cache").
var CacheChoices = []CacheChoice{
	{"30 minutes (default)", 1800},
	{"1 hour", 3600},
	{"2 hours", 7200},
	{"3 hours", 10800},
	{"Once (do not remember)", CacheNever},
	{"Until session expires (forever)", 0},
}

func ValidateSessionPassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinSessionPassword {
		return fmt.Errorf("The session password needs at least %d characters.", MinSessionPassword)
	}
	return nil
}
