package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const defaultWidth = 100

// Item is one marked line; Hint follows it muted, Sub lines sit under it.
type Item struct {
	Mark Mark
	Text string
	Hint string
	Sub  []string
}

// Out prints styled text; colors drop when w is not a terminal or NO_COLOR is set.
type Out struct {
	w     io.Writer
	width int
	// tty cuts long lines to the terminal; logs keep them whole for grep.
	tty bool
}

func New(w io.Writer) *Out {
	o := &Out{w: colorprofile.NewWriter(w, os.Environ()), width: defaultWidth}

	if f, ok := w.(term.File); ok && term.IsTerminal(f.Fd()) {
		o.tty = true

		if cols, _, err := term.GetSize(f.Fd()); err == nil && cols > 0 {
			o.width = cols
		}
	}

	return o
}

func (o *Out) Width() int {
	return o.width
}

func (o *Out) Println(a ...any) {
	fmt.Fprintln(o.w, a...)
}

func (o *Out) Printf(format string, a ...any) {
	fmt.Fprintf(o.w, format, a...)
}

// Header is the first line of a command's output: a bold subject and muted details.
func (o *Out) Header(subject string, details ...string) {
	line := BoldStyle.Render(subject)

	for _, d := range details {
		line += Muted(" · " + d)
	}

	o.Println(line)
}

// Section starts a block with a blank line, a title and an optional summary.
func (o *Out) Section(title, summary string) {
	line := TitleStyle.Render(title)

	if summary != "" {
		line += "  " + summary
	}

	o.Println()
	o.Println(line)
}

// List prints marked items indented under the current section.
func (o *Out) List(items ...Item) {
	if len(items) == 0 {
		return
	}

	// marks parallels the items; the enumerator skips nested Sub lists.
	var marks []Mark
	l := list.New().
		Enumerator(func(_ list.Items, i int) string { return string(marks[i]) }).
		EnumeratorStyleFunc(func(_ list.Items, i int) lipgloss.Style { return markStyles[marks[i]].PaddingRight(1) })

	for _, it := range items {
		text, subs := it.Text, it.Sub

		// A hint that does not fit after the text moves to its own line below it.
		if it.Hint != "" && ansi.StringWidth(text)+3+ansi.StringWidth(it.Hint) <= o.Room(4) {
			text += Muted(" — " + it.Hint)
		} else if it.Hint != "" {
			subs = append([]string{"→ " + it.Hint}, subs...)
		}

		l.Item(o.cut(text, 4))
		marks = append(marks, it.Mark)

		if len(subs) > 0 {
			sub := list.New().Enumerator(func(list.Items, int) string { return "" })

			for _, s := range subs {
				sub.Item(Muted(o.cut(s, 8)))
			}

			l.Item(sub)
		}
	}

	o.Println(indent(l.String(), "  "))
}

// Table prints rows in aligned columns without borders; headers may be nil.
func (o *Out) Table(headers []string, rows [][]string) {
	last := len(headers) - 1

	if len(rows) > 0 {
		last = max(last, len(rows[0])-1)
	}

	t := table.New().
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle()

			if row == table.HeaderRow {
				style = BoldStyle
			}

			if col < last {
				style = style.PaddingRight(3)
			}

			return style
		}).
		Rows(rows...)

	if len(headers) > 0 {
		t.Headers(headers...)
	}

	o.Println(indent(strings.TrimRight(t.String(), "\n"), "  "))
}

// Summary is the closing line: a bold label and comma-separated parts.
func (o *Out) Summary(label string, parts ...string) {
	o.Println()
	o.Println(BoldStyle.Render(label) + " " + strings.Join(parts, ", "))
}

// Done prints a one-line result.
func (o *Out) Done(text string) {
	o.Println(Paint(Done, string(Done)) + " " + text)
}

// Names joins names to fit room columns, ending with "… +N" for the ones left out.
func Names(names []string, room int) string {
	var b strings.Builder

	for i, n := range names {
		more := fmt.Sprintf(" … +%d", len(names)-i)

		if i > 0 && b.Len()+2+len(n)+len(more) > room {
			return b.String() + Muted(more)
		}

		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(n)
	}

	return b.String()
}

// Room is how many columns are left on a line after prefix columns are used.
func (o *Out) Room(prefix int) int {
	return max(20, o.width-prefix)
}

func (o *Out) cut(s string, prefix int) string {
	if !o.tty {
		return s
	}

	return ansi.Truncate(s, o.Room(prefix), "…")
}

func indent(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
