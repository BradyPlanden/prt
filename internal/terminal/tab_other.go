//go:build !darwin

package terminal

import (
	"fmt"
	"os"
)

// Detect returns a fallback opener on non-macOS systems.
func Detect(cfg Config) (TabOpener, error) {
	term := normalizeTerminal(cfg.Terminal)
	if term == "auto" {
		return Printer{Writer: os.Stdout}, nil
	}

	return nil, fmt.Errorf("terminal opening not supported on this OS")
}
