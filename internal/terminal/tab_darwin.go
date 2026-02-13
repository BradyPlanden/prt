//go:build darwin

package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type opener struct {
	app    string
	custom func(path string) error
}

// Detect returns a macOS terminal opener based on configured preference.
func Detect(cfg Config) (TabOpener, error) {
	term := normalizeTerminal(cfg.Terminal)
	if term == "auto" {
		term = detectFromEnv()
	}

	switch term {
	case "iterm", "iterm2", "iterm.app":
		return opener{app: "iTerm", custom: openITerm}, nil
	case "terminal", "terminal.app", "apple_terminal":
		return opener{app: "Terminal", custom: openTerminal}, nil
	case "auto", "", "unknown":
		return Printer{Writer: os.Stdout}, nil
	default:
		return nil, fmt.Errorf("unsupported terminal: %s", cfg.Terminal)
	}
}

func (o opener) Open(path string) error {
	return o.custom(path)
}

func detectFromEnv() string {
	termProgram := os.Getenv("TERM_PROGRAM")
	switch termProgram {
	case "iTerm.app":
		return "iterm2"
	case "Apple_Terminal":
		return "terminal"
	default:
		return "unknown"
	}
}

func openITerm(dir string) error {
	cmd := fmt.Sprintf("cd %s", shellEscape(dir))
	script := fmt.Sprintf(`
		tell application "iTerm"
			activate
			if (count of windows) = 0 then
				create window with default profile
			end if
			tell current window
				create tab with default profile
				tell current session
					write text "%s"
				end tell
			end tell
		end tell
	`, escapeAppleScript(cmd))

	return runAppleScript("iTerm", script)
}

func openTerminal(dir string) error {
	cmd := fmt.Sprintf("cd %s", shellEscape(dir))
	script := fmt.Sprintf(`
		tell application "Terminal"
			activate
			if (count of windows) = 0 then
				do script "%s"
			else
				do script "" in front window
				set newTab to selected tab of front window
				do script "%s" in newTab
			end if
		end tell
	`, escapeAppleScript(cmd), escapeAppleScript(cmd))

	return runAppleScript("Terminal", script)
}

func runAppleScript(app string, script string) error {
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	msg := strings.TrimSpace(string(output))
	if strings.Contains(strings.ToLower(msg), "not authorized") || strings.Contains(strings.ToLower(msg), "not authorised") {
		return PermissionError{App: app, Err: errors.New(msg)}
	}

	return fmt.Errorf("osascript failed: %s", msg)
}

func escapeAppleScript(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
