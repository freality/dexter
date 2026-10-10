package main

import (
	"os"

	"github.com/freality/dexter/cmd/dex/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
