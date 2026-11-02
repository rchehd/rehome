package cli

import (
	"bufio"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var stdin = bufio.NewReader(os.Stdin)

func interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
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
