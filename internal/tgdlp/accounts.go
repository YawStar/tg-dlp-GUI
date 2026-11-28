// Package tgdlp talks to the tg-dlp command line tool: locating the executable,
// listing / removing accounts, whoami checks and argument helpers.

package tgdlp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"tg-dlp_GUI/internal/platform"
	"tg-dlp_GUI/internal/progress"
)

// ============================================================
// Talks to the tg-dlp CLI:
//   list    tg-dlp accounts --list        (parsed)
//   switch  tg-dlp switch <profile>
//   rename  tg-dlp rename <profile> <new>
//   remove  drives the interactive "tg-dlp accounts" menu inside a hidden pty
//           ("-", <number>, "y"), so tg-dlp itself deletes the session
//   add     see accounts_add.go (config-pw, config-cache, login --qr-url -> QR code in the GUI)
// ============================================================
type Account struct {
	Index   int    // 1-based number as printed by "tg-dlp accounts"
	Label   string // "Name (@username)"
	Profile string // profile name ("default" for the unnamed one)
	Current bool
}

// "  3. ● Name (@user)   [profile: work]  (current)"
var accountLineRe = regexp.MustCompile(`^\s*(\d+)\.\s+(?:●\s+)?(.+?)\s+\[profile:\s*([^\]]*)\]\s*(\(current\))?\s*$`)

var ProfileNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func ParseAccounts(out string) []Account {
	var items []Account
	for _, line := range strings.Split(progress.StripANSI(out), "\n") {
		m := accountLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		items = append(items, Account{
			Index:   n,
			Label:   strings.TrimSpace(m[2]),
			Profile: strings.TrimSpace(m[3]),
			Current: m[4] != "",
		})
	}
	return items
}

// RunHidden runs a non-interactive tg-dlp command without any console window.
func RunHidden(timeout time.Duration, args ...string) (string, error) {
	exe, err := ResolvePath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	platform.HideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = errors.New("tg-dlp did not answer in time")
	}
	return strings.TrimSpace(progress.StripANSI(string(out))), err
}

func ListAccounts() ([]Account, error) {
	out, err := RunHidden(20*time.Second, "accounts", "--list")
	items := ParseAccounts(out)
	if err != nil && len(items) == 0 {
		msg := CleanCLIText(out)
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return items, nil
}

// CleanCLIText removes emoji (the UI font can't draw them), blank lines,
// and keeps the last few lines.
func CleanCLIText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0xFE0F || r == 0x200D ||
			(r >= 0x1F000 && r <= 0x1FFFF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) {
			continue
		}
		b.WriteRune(r)
	}
	var lines []string
	for _, ln := range strings.Split(b.String(), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return strings.Join(lines, "\n")
}

// RemoveAccount answers the prompts of the interactive menu
// (menu "-" -> account number -> "y") inside a hidden pseudo terminal.
func RemoveAccount(number int) (string, error) {
	exe, err := ResolvePath()
	if err != nil {
		return "", err
	}
	pty, err := platform.StartPTY(exe, []string{"accounts"})
	if err != nil {
		return "", err
	}

	var (
		mu  sync.Mutex
		buf strings.Builder
	)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		tmp := make([]byte, 4096)
		for {
			n, err := pty.Read(tmp)
			if n > 0 {
				mu.Lock()
				buf.Write(tmp[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return progress.StripANSI(buf.String())
	}
	waitFor := func(s string, d time.Duration) bool {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if strings.Contains(snapshot(), s) {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	send := func(s string) { _, _ = pty.Write([]byte(s + "\r")) }
	abort := func(msg string) (string, error) {
		pty.Kill()
		_ = pty.Close()
		return CleanCLIText(snapshot()), errors.New(msg)
	}

	if !waitFor("Select an account number", 15*time.Second) {
		return abort("tg-dlp did not show the account menu")
	}
	send("-")
	if !waitFor("Remove which account number", 10*time.Second) {
		return abort("tg-dlp did not ask which account to remove")
	}
	send(strconv.Itoa(number))
	if !waitFor("Continue?", 10*time.Second) {
		return abort("tg-dlp did not ask for confirmation")
	}
	send("y")

	done := make(chan struct{})
	go func() {
		_, _ = pty.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		pty.Kill()
	}
	_ = pty.Close()
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
	}
	return CleanCLIText(snapshot()), nil
}

func ProfileExists(items []Account, name string) bool {
	for _, it := range items {
		if it.Profile == name {
			return true
		}
	}
	return false
}

func ValidateProfileName(name string, existing []Account) error {
	switch {
	case name == "":
		return errors.New("Please enter a profile name.")
	case strings.ContainsAny(name, " \t"):
		return errors.New("The profile name can't contain spaces.")
	case utf8.RuneCountInString(name) > 32:
		return errors.New("The profile name can be at most 32 characters.")
	case name == "default":
		return errors.New("'default' is reserved. Please choose another name.")
	case !ProfileNameRe.MatchString(name):
		return errors.New("The profile name can't contain special characters like < > ? | \\ * / : \". Use only letters, digits, '-' and '_'.")
	case ProfileExists(existing, name):
		return fmt.Errorf("An account named %q already exists.", name)
	}
	return nil
}
