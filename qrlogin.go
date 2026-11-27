package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"

	tdaccounts "tg-dlp/core/accounts"
	tdclient "tg-dlp/core/client"
	tdsession "tg-dlp/core/session"
)

// ============================================================
// Native QR login (no tg-dlp process, no pty).
//
// This is the same flow as "tg-dlp login --qr --profile <n>" in
// tg-dlp/cmd/login.go, built on the tg-dlp packages themselves
// (core/client, core/session, core/accounts), so the session file,
// the password cache and the .meta file are exactly what the CLI
// creates and reads. The prompts of the CLI are replaced by hooks
// that the Fyne dialog implements.
// ============================================================

// How long the session password is remembered. 0 = until the session file is
// deleted, qrCacheNever (-1) = ask every time, > 0 = seconds.
const qrCacheNever = -1

type qrStage int

const (
	stageScan   qrStage = iota // waiting for the phone to scan the QR code
	stageSaving                // scanned, finishing the login
)

// qrLoginHooks connects the login to the GUI. Every hook is called from the
// login goroutine, never from the UI thread, so the GUI must use fyne.Do.
type qrLoginHooks struct {
	// ShowQR gets a new login link about every 30 seconds.
	ShowQR func(url string)
	// Stage tells which part of the flow is running.
	Stage func(stage qrStage)
	// Status is a short human readable progress message.
	Status func(msg string)
	// AskPassword asks for the Telegram two-step verification password. It
	// blocks until the user answers and must return when ctx is cancelled.
	AskPassword func(ctx context.Context, retry bool) (string, error)
}

type qrLoginParams struct {
	ConfigDir    string // ~/.tg-dlp
	Profile      string // new profile name, already validated
	Password     string // new session password
	CacheSeconds int    // see qrCacheNever
	Proxy        string // "" = none given
	NoProxy      bool
}

// tgDlpConfigDir is the folder tg-dlp keeps its data in (same rule as the CLI).
func tgDlpConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the user home directory: %w", err)
	}
	return filepath.Join(home, ".tg-dlp"), nil
}

func qrFileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// loadAPICredentials reads the API ID / hash tg-dlp saved in config.enc
// (same file and same fixed password as tg-dlp/cmd/root.go loadConfig).
func loadAPICredentials(configDir string) (int, string, error) {
	const configPassword = "tg-dlp"
	path := filepath.Join(configDir, "config.enc")

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// tg-dlp creates config.enc from its built-in credentials the first
		// time it runs. "login" does that and then stops by itself because
		// there is no terminal, so nothing is changed besides config.enc.
		_, _ = runTgDlpHidden(20*time.Second, "login")
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return 0, "", errors.New("the Telegram API credentials (" + path + ") were not found. " +
			"Run \"tg-dlp login\" once in a terminal and enter your API ID / hash, then try again")
	}

	plain, err := tdsession.DecryptData(data, configPassword)
	if err != nil {
		return 0, "", errors.New("cannot read " + path + " (wrong password or corrupted file)")
	}
	parts := strings.Split(string(plain), "|")
	if len(parts) != 2 {
		return 0, "", errors.New(path + " has an unexpected format")
	}
	id, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	hash := strings.TrimSpace(parts[1])
	if id <= 0 || hash == "" {
		return 0, "", errors.New("the saved API credentials are invalid. Delete " + path + " and run tg-dlp again")
	}
	return id, hash, nil
}

// cleanupFailedAccount removes everything a login that did not finish left
// behind, so no empty profile shows up in the list.
func cleanupFailedAccount(sessionPath string) {
	_ = os.Remove(sessionPath)
	_ = os.Remove(sessionPath + ".tmp")
	_ = os.Remove(tdaccounts.MetaPath(sessionPath))
	tdsession.ClearPasswordCache(sessionPath)
	tdsession.ClearPendingPassword(sessionPath)
	_ = os.Remove(filepath.Dir(sessionPath)) // only succeeds when the folder is empty
}

// runQRLogin logs a new account in with a QR code, saves its encrypted
// session under profiles/<name>/ and makes it the active account.
// It returns when the login is finished, failed or ctx is cancelled.
func runQRLogin(ctx context.Context, p qrLoginParams, h qrLoginHooks) error {
	status := func(msg string) {
		if h.Status != nil {
			h.Status(msg)
		}
	}

	appID, appHash, err := loadAPICredentials(p.ConfigDir)
	if err != nil {
		return err
	}

	sessionPath := tdaccounts.SessionPath(p.ConfigDir, p.Profile)
	if qrFileExists(sessionPath) {
		return fmt.Errorf("an account named %q already exists", p.Profile)
	}
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
		return err
	}

	success := false
	defer func() {
		// The parked password must never stay on disk, whatever happened.
		tdsession.ClearPendingPassword(sessionPath)
		if !success {
			cleanupFailedAccount(sessionPath)
		}
	}()

	// GetClientWithHandler uses a parked password for a NEW session without
	// asking anything on the terminal (this is what "tg-dlp config-pw" does).
	if err := tdsession.SavePendingPassword(sessionPath, p.Password); err != nil {
		return fmt.Errorf("cannot prepare the session: %w", err)
	}

	dispatcher := tg.NewUpdateDispatcher()
	cli, err := tdclient.GetClientWithHandler(appID, appHash, sessionPath, p.NoProxy, p.Proxy, dispatcher)
	if err != nil {
		return err
	}

	status("Connecting to Telegram...")
	runErr := cli.Run(ctx, func(ctx context.Context) error {
		if err := guiQRAuth(ctx, cli, dispatcher, h); err != nil {
			return err
		}
		tdclient.RememberAccount(ctx, cli) // saves the display name (.meta)
		success = true
		return nil
	})
	if !success {
		if runErr == nil {
			runErr = errors.New("the login did not finish")
		}
		return runErr
	}
	if !qrFileExists(sessionPath) {
		success = false
		return errors.New("the session file was not saved")
	}

	// "How long to remember the password": same effect as "tg-dlp config-cache".
	switch {
	case p.CacheSeconds == qrCacheNever:
		tdsession.ClearPasswordCache(sessionPath)
	case p.CacheSeconds == 0:
		tdsession.SavePasswordToCache(sessionPath, p.Password, 100*365*24*time.Hour)
	default:
		tdsession.SavePasswordToCache(sessionPath, p.Password, time.Duration(p.CacheSeconds)*time.Second)
	}

	// The new account becomes the current one ("tg-dlp switch <name>").
	if err := tdaccounts.SetActive(p.ConfigDir, p.Profile); err != nil {
		return fmt.Errorf("the account was added, but it could not be selected: %w", err)
	}
	return nil
}

// guiQRAuth is loginWithQR from tg-dlp/cmd/login.go with GUI hooks.
func guiQRAuth(ctx context.Context, cli *telegram.Client, dispatcher tg.UpdateDispatcher, h qrLoginHooks) error {
	loggedIn := qrlogin.OnLoginToken(dispatcher)

	err := guiQRAuthWithRetry(ctx, cli.QR(), loggedIn, h)
	if err == nil {
		return nil
	}

	// Two-step verification: the QR code was accepted, now the cloud password is needed.
	needPassword := errors.Is(err, auth.ErrPasswordAuthNeeded) ||
		strings.Contains(err.Error(), "SESSION_PASSWORD_NEEDED") ||
		strings.Contains(err.Error(), "2FA required")
	if !needPassword {
		return err
	}
	if h.AskPassword == nil {
		return errors.New("this account uses two-step verification, which cannot be answered here")
	}

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pw, perr := h.AskPassword(ctx, attempt > 1)
		if perr != nil {
			return perr
		}
		if _, err = cli.Auth().Password(ctx, pw); err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "PASSWORD_HASH_INVALID") {
			return err
		}
	}
	return errors.New("wrong two-step verification password")
}

// guiQRAuthWithRetry is qrAuthWithRetry from tg-dlp/cmd/login.go: qr.Auth plus
// a fresh-token retry when Telegram moves the account to another data centre
// (AUTH_TOKEN_EXPIRED). The QR code does not have to be scanned again.
func guiQRAuthWithRetry(ctx context.Context, qr qrlogin.QR, loggedIn qrlogin.LoggedIn, h qrLoginHooks) error {
	const importAttempts = 4

	for {
		token, err := qr.Export(ctx)
		if err != nil {
			return err
		}
		if h.ShowQR != nil {
			h.ShowQR(token.URL())
		}
		if h.Stage != nil {
			h.Stage(stageScan)
		}

		wait := time.Until(token.Expires())
		if wait < time.Second {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		scanned := false
		select {
		case <-loggedIn:
			scanned = true
		case <-timer.C: // expired unscanned: show a fresh QR code
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		timer.Stop()
		if !scanned {
			continue
		}

		if h.Stage != nil {
			h.Stage(stageSaving)
		}
		var lastErr error
		for attempt := 1; attempt <= importAttempts; attempt++ {
			_, err := qr.Import(ctx)
			if err == nil {
				return nil
			}
			if !strings.Contains(err.Error(), "AUTH_TOKEN_EXPIRED") {
				return err
			}
			lastErr = err
			if h.Status != nil {
				h.Status(fmt.Sprintf("Telegram token expired while switching data centre (%d/%d), retrying...", attempt, importAttempts))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		return lastErr
	}
}

var qrFloodRe = regexp.MustCompile(`FLOOD_(?:PREMIUM_)?WAIT[_\s(]*(\d+)`)

// friendlyLoginError turns the common Telegram errors into plain sentences
// (same cases as explainLoginError in tg-dlp/cmd/login.go, without emoji).
func friendlyLoginError(err error) string {
	s := err.Error()
	switch {
	case errors.Is(err, context.Canceled):
		return "Login cancelled."
	case strings.Contains(s, "PHONE_NUMBER_FLOOD"):
		return "Telegram has blocked this phone number temporarily (too many login attempts). Wait at least 24 hours."
	case strings.Contains(s, "FLOOD_WAIT"), strings.Contains(s, "FLOOD_PREMIUM_WAIT"):
		wait := "a while"
		if m := qrFloodRe.FindStringSubmatch(s); len(m) > 1 {
			wait = m[1] + " seconds"
		}
		return "Telegram asked you to wait " + wait + " before trying again."
	case strings.Contains(s, "PASSWORD_HASH_INVALID"):
		return "Wrong two-step verification password."
	case strings.Contains(s, "API_ID_PUBLISHED_FLOOD"):
		return "This API ID/hash is blocked because too many people use it. Create your own at https://my.telegram.org."
	case strings.Contains(s, "API_ID_INVALID"):
		return "The API ID / API hash is invalid. Delete ~/.tg-dlp/config.enc and enter them again."
	}
	return s
}
