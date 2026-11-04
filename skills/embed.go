// Package skills embeds the agent skills shipped in this directory, in the
// skills.sh layout (skills/<name>/SKILL.md), so the binary installs the skill
// that matches its own version.
package skills

import "embed"

// FS holds the ytrack skill under the path "ytrack".
//
//go:embed ytrack
var FS embed.FS
