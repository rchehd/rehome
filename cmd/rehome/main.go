package main

import (
	"os"

	"github.com/rchehd/rehome/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
