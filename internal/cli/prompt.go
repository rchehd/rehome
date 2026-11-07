package cli

import (
	"bufio"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/ui"
)

var stdin = bufio.NewReader(os.Stdin)

func interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// confirm asks a yes/no question with huh in a terminal and as a plain line otherwise.
func confirm(cmd *cobra.Command, question string, def bool) bool {
	if !interactive() {
		hint, fallback := "[y/N]", "n"

		if def {
			hint, fallback = "[Y/n]", "y"
		}

		return ask(cmd, question+" "+hint, fallback) == "y"
	}

	ok, err := ui.Confirm(question, def)

	if err != nil {
		cmd.PrintErrln("error:", err)

		return false
	}

	return ok
}

// ask prints the question and returns the lowercased first letter of the answer, or def.
func ask(cmd *cobra.Command, question, def string) string {
	cmd.Print(question + " ")
	line, err := stdin.ReadString('\n')

	if err != nil {
		return def
	}

	line = strings.ToLower(strings.TrimSpace(line))

	if line == "" {
		return def
	}

	return line[:1]
}
