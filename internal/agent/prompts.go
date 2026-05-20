package agent

import _ "embed"

// SystemPrompt is the base agent system prompt (SPECS §9).
//
//go:embed prompts/system.md
var SystemPrompt string
