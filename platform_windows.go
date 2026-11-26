//go:build windows

package main

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"

	conpty "github.com/EgeBalci/conpty-go"
)

type winPTY struct {
	c *conpty.ConPty
}

func startPTY(exePath string, args []string) (ptySession, error) {
	c, err := conpty.Start(
		buildCommandLine(exePath, args),
		conpty.ConPtyDimensions(160, 40),
	)
	if err != nil {
		return nil, err
	}
	return &winPTY{c: c}, nil
}

func (p *winPTY) Read(b []byte) (int, error)  { return p.c.Read(b) }
func (p *winPTY) Write(b []byte) (int, error) { return p.c.Write(b) }

func (p *winPTY) Wait() (int, error) {
	code, err := p.c.Wait(context.Background())
	return int(code), err
}

// Interrupt sends Ctrl+C (ETX) through the pseudo console.
func (p *winPTY) Interrupt() {
	_, _ = p.c.Write([]byte{0x03})
}

// Kill force-kills tg-dlp and its children. It targets only our own process
// tree (by PID) so other running tg-dlp instances are left alone.
func (p *winPTY) Kill() {
	args := []string{"/F", "/T", "/IM", "tg-dlp.exe"} // fallback if PID is unavailable
	if pp, ok := interface{}(p.c).(interface{ Pid() int }); ok {
		if pid := pp.Pid(); pid > 0 {
			args = []string{"/F", "/T", "/PID", strconv.Itoa(pid)}
		}
	}
	kill := exec.Command("taskkill", args...)
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = kill.Run()
}

func (p *winPTY) Close() error {
	p.c.Close()
	return nil
}

// runInTerminal opens a new console window running tg-dlp (login / accounts).
func runInTerminal(exePath string, args []string) error {
	cmd := exec.Command("cmd.exe", "/c", "start", "cmd.exe", "/k", buildCommandLine(exePath, args))
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}
