package policy

import "github.com/mdombrov-33/relay/internal/tool"

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

type Allowlist struct {
	toolNames map[string]struct{}
}

func NewAllowlist(toolNames ...string) Allowlist {
	allowed := make(map[string]struct{}, len(toolNames))
	for _, toolName := range toolNames {
		if toolName != "" {
			allowed[toolName] = struct{}{}
		}
	}

	return Allowlist{toolNames: allowed}
}

func (a Allowlist) Decide(call tool.Call) Decision {
	if _, allowed := a.toolNames[call.Name]; allowed {
		return DecisionAllow
	}

	return DecisionDeny
}
