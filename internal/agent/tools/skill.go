package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"yoli/internal/agent/skills"
	"yoli/internal/ai"
)

// SkillTool serves the bodies of pre-loaded skills so the agent can fetch
// a skill's full instructions on demand. The system prompt advertises
// names, descriptions, and triggers only (see skills.InjectSection).
type SkillTool struct {
	available []skills.LoadedSkill
}

// NewSkillTool constructs a SkillTool over the pre-loaded skill set.
func NewSkillTool(available []skills.LoadedSkill) *SkillTool {
	return &SkillTool{available: available}
}

// Definition returns the tool schema sent to the model.
func (t *SkillTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Skill",
		Description: "Load the full instructions for a skill listed in the system " +
			"prompt's Available Skills section. Returns the skill's Markdown body; " +
			"adopt it as your methodology for the current task.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name of the skill to load, as listed under Available Skills.",
				},
			},
			"required": []string{"name"},
		},
	}
}

type skillArgs struct {
	Name string `json:"name"`
}

// Run returns the frontmatter-stripped body of the named skill. Unknown
// names error with the list of available skills so the model can recover.
func (t *SkillTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args skillArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("skill: invalid arguments: %w", err)
	}
	body, err := skills.Expand(args.Name, t.available)
	if err != nil {
		names := make([]string, 0, len(t.available))
		for _, s := range t.available {
			names = append(names, s.Name)
		}
		return "", fmt.Errorf("%w. Available skills: %s", err, strings.Join(names, ", "))
	}
	return body, nil
}

var _ Tool = (*SkillTool)(nil)
