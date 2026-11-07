package main

import (
	"os"

	"github.com/idarciooliveira/namecheap-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
