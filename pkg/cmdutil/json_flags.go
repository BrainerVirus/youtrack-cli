package cmdutil

// Adapted from github.com/cli/cli pkg/cmdutil/json_flags.go
// (Copyright (c) 2019 GitHub Inc., MIT); see NOTICE.

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
)

// JSONOption adjusts AddJSONFlags.
type JSONOption func(*jsonOptions)

type jsonOptions struct{ jqShorthand string }

// WithoutJQShorthand leaves --jq without its -q shorthand, for commands
// where -q means something else (issue list uses it for --query).
func WithoutJQShorthand() JSONOption {
	return func(o *jsonOptions) { o.jqShorthand = "" }
}

// AddJSONFlags adds --json <fields>, --jq and --template to cmd. After flag
// parsing, *target is nil unless --json was given, in which case it holds an
// exporter limited to the requested fields. Requesting a field outside
// fields, or --jq/--template without --json, is a usage error.
func AddJSONFlags(cmd *cobra.Command, target **output.Exporter, fields []string, opts ...JSONOption) {
	o := jsonOptions{jqShorthand: "q"}
	for _, opt := range opts {
		opt(&o)
	}
	f := cmd.Flags()
	f.StringSlice("json", nil, "Output JSON with the specified `fields`")
	f.StringP("jq", o.jqShorthand, "", "Filter JSON output using a jq `expression`")
	f.StringP("template", "t", "", "Format JSON output using a Go `template`")

	sorted := slices.Clone(fields)
	sort.Strings(sorted)

	_ = cmd.RegisterFlagCompletionFunc("json", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		prefix := ""
		if i := strings.LastIndexByte(toComplete, ','); i >= 0 {
			prefix, toComplete = toComplete[:i+1], toComplete[i+1:]
		}
		var out []string
		for _, f := range sorted {
			if strings.HasPrefix(strings.ToLower(f), strings.ToLower(toComplete)) {
				out = append(out, prefix+f)
			}
		}
		return out, cobra.ShellCompDirectiveNoSpace
	})

	prev := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(c, args); err != nil {
				return err
			}
		}
		e, err := checkJSONFlags(c.Flags())
		if err != nil {
			return err
		}
		if e != nil {
			for _, name := range e.Fields {
				if !slices.Contains(fields, name) {
					return clierr.FlagErrorf("unknown JSON field: %q\nAvailable fields:\n  %s", name, strings.Join(sorted, "\n  "))
				}
			}
		}
		*target = e
		return nil
	}

	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if c == cmd && err.Error() == "flag needs an argument: --json" {
			return clierr.FlagErrorf("specify one or more comma-separated fields for `--json`:\n  %s", strings.Join(sorted, "\n  "))
		}
		return clierr.FlagErrorWrap(err)
	})

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["help:json-fields"] = strings.Join(sorted, ",")
}

func checkJSONFlags(f *pflag.FlagSet) (*output.Exporter, error) {
	jsonFlag, jqFlag, tplFlag := f.Lookup("json"), f.Lookup("jq"), f.Lookup("template")
	switch {
	case jsonFlag.Changed:
		if jqFlag.Changed && tplFlag.Changed {
			return nil, clierr.FlagErrorf("cannot use `--jq` and `--template` together")
		}
		fields := jsonFlag.Value.(pflag.SliceValue).GetSlice()
		if len(fields) == 0 {
			return nil, clierr.FlagErrorWrap(errors.New("flag needs an argument: --json"))
		}
		return &output.Exporter{
			Fields: fields,
			Format: output.Format{JQ: jqFlag.Value.String(), Template: tplFlag.Value.String()},
		}, nil
	case jqFlag.Changed:
		return nil, clierr.FlagErrorf("cannot use `--jq` without specifying `--json`")
	case tplFlag.Changed:
		return nil, clierr.FlagErrorf("cannot use `--template` without specifying `--json`")
	}
	return nil, nil
}
