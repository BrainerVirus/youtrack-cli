package root

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
)

// helpFunc renders help like cobra's default template, with bold section
// headings, command names and flags in the accent color and example commands
// highlighted. Without color the output is byte-identical to cobra's.
func helpFunc(ios *iostreams.IOStreams) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, _ []string) {
		renderHelp(cmd.OutOrStdout(), cmd, ios.ColorScheme())
	}
}

var (
	// A heading inside Long text: "Environment:", "Exit codes:", "JSON fields:".
	longHeading = regexp.MustCompile(`^[A-Z][A-Za-z ]*:$`)
	// A pflag usage line: indent, optional "-x, ", "--name", optional type.
	flagLine = regexp.MustCompile(`^(\s+)((?:-[^-\s], )?--[A-Za-z0-9][\w-]*)( [^\s]+)?(\s{2,}.*)?$`)
	// An example line: indent, "$ ", the command, an optional "  # comment"
	// (a "#" inside the command, as in a query, has no space after it).
	exampleLine = regexp.MustCompile(`^(\s*)\$ (.*?)(\s+# .*)?$`)
	// An environment variable entry in Long text.
	envLine = regexp.MustCompile(`^(\s+)([A-Z][A-Z0-9_]+)(\s{2,}.*)$`)
)

func renderHelp(w io.Writer, cmd *cobra.Command, cs *iostreams.ColorScheme) {
	var b strings.Builder
	if text := strings.TrimRight(cmd.Long, " \t\n"); text != "" {
		b.WriteString(styleLong(text, cs) + "\n\n")
	} else if text := strings.TrimRight(cmd.Short, " \t\n"); text != "" {
		b.WriteString(text + "\n\n")
	}
	if cmd.Runnable() || cmd.HasSubCommands() {
		b.WriteString(usage(cmd, cs))
	}
	fmt.Fprint(w, b.String())
}

func styleLong(text string, cs *iostreams.ColorScheme) string {
	if !cs.Enabled() {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		switch {
		case longHeading.MatchString(l):
			lines[i] = cs.Bold(l)
		case envLine.MatchString(l):
			m := envLine.FindStringSubmatch(l)
			lines[i] = m[1] + cs.Accent(m[2]) + m[3]
		case flagLine.MatchString(l):
			lines[i] = styleFlag(l, cs)
		}
	}
	return strings.Join(lines, "\n")
}

func usage(cmd *cobra.Command, cs *iostreams.ColorScheme) string {
	var b strings.Builder
	b.WriteString(cs.Bold("Usage:"))
	if cmd.Runnable() {
		b.WriteString("\n  " + styleUseLine(cmd.UseLine(), cmd.CommandPath(), cs))
	}
	if cmd.HasAvailableSubCommands() {
		b.WriteString("\n  " + cs.Accent(cmd.CommandPath()) + " [command]")
	}
	if len(cmd.Aliases) > 0 {
		b.WriteString("\n\n" + cs.Bold("Aliases:") + "\n  " + cmd.NameAndAliases())
	}
	if cmd.HasExample() {
		b.WriteString("\n\n" + cs.Bold("Examples:") + "\n" + styleExamples(cmd.Example, cs))
	}
	if cmd.HasAvailableSubCommands() {
		b.WriteString("\n\n" + cs.Bold("Available Commands:"))
		for _, c := range cmd.Commands() {
			if c.IsAvailableCommand() || c.Name() == "help" {
				b.WriteString("\n  " + cs.Accent(rpad(c.Name(), c.NamePadding())) + " " + c.Short)
			}
		}
	}
	if cmd.HasAvailableLocalFlags() {
		b.WriteString("\n\n" + cs.Bold("Flags:") + "\n" + styleFlags(cmd.LocalFlags().FlagUsages(), cs))
	}
	if cmd.HasAvailableInheritedFlags() {
		b.WriteString("\n\n" + cs.Bold("Global Flags:") + "\n" + styleFlags(cmd.InheritedFlags().FlagUsages(), cs))
	}
	if cmd.HasHelpSubCommands() {
		b.WriteString("\n\n" + cs.Bold("Additional help topics:"))
		for _, c := range cmd.Commands() {
			if c.IsAdditionalHelpTopicCommand() {
				b.WriteString("\n  " + cs.Accent(rpad(c.CommandPath(), c.CommandPathPadding())) + " " + c.Short)
			}
		}
	}
	if cmd.HasAvailableSubCommands() {
		b.WriteString("\n\n" + cs.Dim(`Use "`+cmd.CommandPath()+` [command] --help" for more information about a command.`))
	}
	b.WriteString("\n")
	return b.String()
}

// styleUseLine accents the command path at the start of a use line.
func styleUseLine(line, path string, cs *iostreams.ColorScheme) string {
	if rest, ok := strings.CutPrefix(line, path); ok {
		return cs.Accent(path) + rest
	}
	return line
}

func styleExamples(ex string, cs *iostreams.ColorScheme) string {
	if !cs.Enabled() {
		return ex
	}
	lines := strings.Split(ex, "\n")
	for i, l := range lines {
		if m := exampleLine.FindStringSubmatch(l); m != nil {
			lines[i] = m[1] + cs.Dim("$") + " " + cs.Accent(m[2]) + cs.Dim(m[3])
		}
	}
	return strings.Join(lines, "\n")
}

func styleFlags(usages string, cs *iostreams.ColorScheme) string {
	usages = strings.TrimRight(usages, " \t\n")
	if !cs.Enabled() {
		return usages
	}
	lines := strings.Split(usages, "\n")
	for i, l := range lines {
		lines[i] = styleFlag(l, cs)
	}
	return strings.Join(lines, "\n")
}

func styleFlag(l string, cs *iostreams.ColorScheme) string {
	m := flagLine.FindStringSubmatch(l)
	if m == nil {
		return l
	}
	return m[1] + cs.Accent(m[2]) + cs.Dim(m[3]) + m[4]
}

func rpad(s string, n int) string {
	return fmt.Sprintf("%-*s", n, s)
}
