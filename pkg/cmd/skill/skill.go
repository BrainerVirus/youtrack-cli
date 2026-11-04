// Package skill implements `ytrack skill`: the agent skill embedded in the
// binary, printed or installed for a coding agent.
package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdSkill returns the skill command group.
func NewCmdSkill(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill <command>",
		Short: "Print or install the ytrack agent skill",
		Long: `Print or install the agent skill that teaches coding agents (Claude Code,
Codex, Cursor, OpenCode, Pi and others) how to use ytrack. The skill is built
into this binary and stamped with its version, so it always matches the
ytrack you run. These commands work offline and without logging in.`,
		RunE: cmdutil.GroupRunE,
	}
	cmd.AddCommand(newCmdPrint(f))
	cmd.AddCommand(newCmdInstall(f))
	return cmd
}

func newCmdPrint(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "print",
		Short: "Print the skill's SKILL.md",
		Long: `Print the skill's SKILL.md, with metadata.version set to this ytrack's
version. The skill's reference files are written only by ytrack skill install.`,
		Example: `  $ ytrack skill print
  $ ytrack skill print > ~/skills/ytrack/SKILL.md`,
		Args: cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			b, err := skill.Render(build.Version)
			if err != nil {
				return err
			}
			_, err = f.IOStreams.Out.Write(b)
			return err
		},
	}
}

func newCmdInstall(f *cmdutil.Factory) *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "install --dir <skills-dir>",
		Short: "Install the skill into an agent's skills directory",
		Long: `Write the skill to <skills-dir>/ytrack: SKILL.md, stamped with this ytrack's
version, and its references/ directory. <skills-dir> is created when
missing. Run it again after an upgrade to update the skill.

An existing <skills-dir>/ytrack is replaced only when its SKILL.md has
"name: ytrack"; any other directory there is refused and left untouched. A
symlink there, as npx skills add makes, is followed. The new copy is written
next to the old one and renamed into place, so a failed install changes
nothing.

Skills directories by agent:
  Claude Code  ~/.claude/skills (or .claude/skills in a project)
  Codex        ~/.codex/skills
  Cursor       ~/.cursor/skills
  OpenCode     ~/.config/opencode/skills
  Pi           ~/.pi/agent/skills
  others       ~/.agents/skills

Prints the skill directory. JSON fields (--json):
  path     the skill directory, e.g. /home/me/.claude/skills/ytrack
  version  the version stamped into SKILL.md`,
		Example: `  $ ytrack skill install --dir ~/.claude/skills
  $ ytrack skill install --dir .claude/skills --json`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("dir") || dir == "" {
				return clierr.FlagErrorf("`--dir` is required, e.g. --dir ~/.claude/skills")
			}
			path, err := skill.Install(expandHome(dir), build.Version)
			if foreign, ok := errors.AsType[*skill.ForeignError](err); ok {
				return fmt.Errorf("%w; remove it or choose another --dir", foreign)
			}
			if err != nil {
				return fmt.Errorf("installing the skill: %w", err)
			}
			if jsonOut {
				return json.NewEncoder(f.IOStreams.Out).Encode(struct {
					Path    string `json:"path"`
					Version string `json:"version"`
				}{path, build.Version})
			}
			fmt.Fprintln(f.IOStreams.Out, path)
			cs := f.IOStreams.ErrColorScheme()
			fmt.Fprintf(f.IOStreams.ErrOut, "%s Installed the ytrack skill %s\n", cs.SuccessIcon(), build.Version)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&dir, "dir", "", "Agent skills `directory` to install into, e.g. ~/.claude/skills")
	fl.BoolVar(&jsonOut, "json", false, "Print {path, version} as JSON")
	return cmd
}

// expandHome expands a leading "~" (as in --dir=~/.claude/skills, which most
// shells leave alone) to the home directory.
func expandHome(dir string) string {
	rest, ok := strings.CutPrefix(dir, "~")
	if !ok || (rest != "" && rest[0] != '/' && rest[0] != filepath.Separator) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	return filepath.Join(home, rest)
}
