package claude

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// WebSearchMaxUses maps OpenAI search_context_size values to Anthropic max_uses.
const (
	WebSearchMaxUsesLow    = 3
	WebSearchMaxUsesMedium = 10
	WebSearchMaxUsesHigh   = 25
)

// mapToolChoice converts an OpenAI tool_choice and parallel_tool_calls into a
// Claude-compatible tool_choice value.
func mapToolChoice(toolChoice any, parallelToolCalls *bool) any {
	if toolChoice == nil {
		if parallelToolCalls != nil && !*parallelToolCalls {
			return &dto.ClaudeToolChoice{Type: "auto", DisableParallelToolUse: true}
		}
		return nil
	}

	switch v := toolChoice.(type) {
	case string:
		switch v {
		case "none":
			return nil
		case "required":
			return &dto.ClaudeToolChoice{Type: "any"}
		case "auto":
			if parallelToolCalls != nil && !*parallelToolCalls {
				return &dto.ClaudeToolChoice{Type: "auto", DisableParallelToolUse: true}
			}
			return &dto.ClaudeToolChoice{Type: "auto"}
		}
	case map[string]any:
		if v["type"] == "function" {
			if fn, ok := v["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok {
					return &dto.ClaudeToolChoice{Type: "tool", Name: name}
				}
			}
		}
	}
	return nil
}
