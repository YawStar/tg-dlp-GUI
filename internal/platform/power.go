package platform

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// PowerAction is something the computer can be told to do.
type PowerAction int

const (
	PowerShutdown PowerAction = iota
	PowerReboot
	PowerSleep
)

// Power runs the action right now. It returns an error when the command
// could not be started or reported a failure (for example when the system
// does not allow it for the current user).
func Power(action PowerAction) error {
	name, args, err := powerCommand(action)
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	HideConsole(cmd) // no flashing console window on Windows
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s failed: %s", name, msg)
	}
	return nil
}

func powerCommand(action PowerAction) (string, []string, error) {
	switch runtime.GOOS {
	case "windows":
		switch action {
		case PowerShutdown:
			return "shutdown", []string{"/s", "/t", "0"}, nil
		case PowerReboot:
			return "shutdown", []string{"/r", "/t", "0"}, nil
		case PowerSleep:
			// Note: Windows hibernates instead of sleeping when hibernation is enabled.
			return "rundll32.exe", []string{"powrprof.dll,SetSuspendState", "0,1,0"}, nil
		}
	case "darwin":
		verb := map[PowerAction]string{PowerShutdown: "shut down", PowerReboot: "restart", PowerSleep: "sleep"}[action]
		return "osascript", []string{"-e", `tell application "System Events" to ` + verb}, nil
	default: // linux and other unix systems with systemd
		verb := map[PowerAction]string{PowerShutdown: "poweroff", PowerReboot: "reboot", PowerSleep: "suspend"}[action]
		return "systemctl", []string{verb}, nil
	}
	return "", nil, fmt.Errorf("unsupported power action")
}
