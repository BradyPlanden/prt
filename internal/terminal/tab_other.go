//go:build !darwin

package terminal

import (
	"fmt"
	"os"
)

func Detect(cfg Config) (TabOpener, error) {
	term := normalizeTerminal(cfg.Terminal)
	if term == "auto" {
		return Printer{Writer: os.Stdout}, nil
	}

	return nil, fmt.Errorf("terminal opening not supported on this OS")
}
