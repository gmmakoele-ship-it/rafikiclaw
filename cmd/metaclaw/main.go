package main

import (
	"os"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
