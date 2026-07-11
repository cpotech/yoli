// Package builtinskills embeds the skill bundle at the repo's skills/
// directory into the yoli binary, so built-in skills are available
// wherever the binary runs — no directory next to the executable needed.
// Adding a new <name>/SKILL.md here and rebuilding is all it takes to
// ship a new built-in.
package builtinskills

import "embed"

// FS holds every */SKILL.md under skills/, rooted so that a skill named
// "plan" appears at "plan/SKILL.md".
//
//go:embed */SKILL.md
var FS embed.FS
