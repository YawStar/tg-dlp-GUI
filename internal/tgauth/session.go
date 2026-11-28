package tgauth

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	tdaccounts "tg-dlp/core/accounts"
	tdsession "tg-dlp/core/session"
)

// ============================================================
// Change the session password / "remember password" time of one
// account. This replaces the old "config-pw" and "config-cache"
// buttons of the Settings dialog.
//
// It works on the session file directly (tg-dlp/core/session), like
// "tg-dlp config-session-password" does, so it does not need a
// terminal to ask for the current password.
// ============================================================

// accountProfileKey converts the profile name shown in the list ("default")
// into the key tg-dlp's accounts package uses ("" = the default slot).
func accountProfileKey(profile string) string {
	if profile == "default" {
		return ""
	}
	return profile
}

// applySessionCache sets how long the session password is remembered
// (same meaning as in qrlogin.go: 0 = until the session file is deleted,
// CacheNever = ask every time, > 0 = seconds).
func applySessionCache(sessionPath, password string, seconds int) {
	switch {
	case seconds == CacheNever:
		tdsession.ClearPasswordCache(sessionPath)
	case seconds == 0:
		tdsession.SavePasswordToCache(sessionPath, password, 100*365*24*time.Hour) // 100 years
	default:
		tdsession.SavePasswordToCache(sessionPath, password, time.Duration(seconds)*time.Second)
	}
}

// ChangeSessionPassword re-encrypts the session of a profile with a new
// password. current may be empty when the old password is still remembered.
func ChangeSessionPassword(configDir, profile, current, newPass string, cacheSeconds int) error {
	if utf8.RuneCountInString(newPass) < MinSessionPassword {
		return fmt.Errorf("The new password needs at least %d characters.", MinSessionPassword)
	}
	sessionPath := tdaccounts.SessionPath(configDir, accountProfileKey(profile))
	if !qrFileExists(sessionPath) {
		return errors.New("This account no longer exists.")
	}
	ctx := context.Background()

	// 1. open the session with the current password
	var data []byte
	if current != "" {
		st := &tdsession.EncryptedSessionStorage{Path: sessionPath, Password: current}
		d, err := st.LoadSession(ctx)
		if err != nil {
			return errors.New("The current session password is wrong.")
		}
		data = d
	} else if cached, ok := tdsession.LoadPasswordFromCache(sessionPath); ok {
		st := &tdsession.EncryptedSessionStorage{Path: sessionPath, Password: cached}
		if d, err := st.LoadSession(ctx); err == nil {
			data = d
		}
	}
	if data == nil {
		return errors.New("Please enter the current session password (it is no longer remembered).")
	}

	// 2. store it again with the new password (temp file + rename)
	newStore := &tdsession.EncryptedSessionStorage{Path: sessionPath, Password: newPass}
	if err := newStore.StoreSession(ctx, data); err != nil {
		return fmt.Errorf("Could not save the session: %w", err)
	}
	// 3. read it back before telling the user it worked
	if _, err := newStore.LoadSession(ctx); err != nil {
		return errors.New("The session was saved, but reading it back with the new password failed. Try again.")
	}

	// 4. the cached copy holds the OLD password
	tdsession.ClearPasswordCache(sessionPath)
	tdsession.ClearPendingPassword(sessionPath)
	applySessionCache(sessionPath, newPass, cacheSeconds)
	return nil
}
