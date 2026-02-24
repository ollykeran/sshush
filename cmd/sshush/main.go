package main

import (
	"os"

	"github.com/ollykeran/sshush/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
