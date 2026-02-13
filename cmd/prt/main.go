package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/BradyPlanden/prt/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.Execute(resolveVersion()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolveVersion() string {
	if version != "" && version != "dev" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}
