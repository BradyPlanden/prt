package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type Config struct {
	Terminal string
}

type TabOpener interface {
	Open(path string) error
}

type Printer struct {
	Writer io.Writer
}

func (p Printer) Open(path string) error {
	if p.Writer == nil {
		p.Writer = os.Stdout
	}
	_, err := fmt.Fprintln(p.Writer, path)
	return err
}

type PermissionError struct {
	App string
	Err error
}

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
