package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Config controls terminal opener selection.
type Config struct {
	Terminal string
}

// TabOpener opens a terminal tab or prints a fallback path.
type TabOpener interface {
	Open(path string) error
}

// Printer is a TabOpener that writes the path to an io.Writer.
type Printer struct {
	Writer io.Writer
}

// Open writes path to the configured writer.
func (p Printer) Open(path string) error {
	if p.Writer == nil {
		p.Writer = os.Stdout
	}
	_, err := fmt.Fprintln(p.Writer, path)
	return err
}

// PermissionError indicates OS automation permissions are missing.
type PermissionError struct {
	App string
	Err error
}

// Error formats a human-readable permission failure message.
func (e PermissionError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("not authorized to control %s", e.App)
	}
	return fmt.Sprintf("not authorized to control %s: %v", e.App, e.Err)
}

func normalizeTerminal(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "auto"
	}
	return value
}
