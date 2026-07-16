package policy

import "github.com/mdombrov-33/relay/internal/tool"

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionRequireApproval Decision = "require_approval"
)

type Allowlist struct {
	authorities map[tool.Authority]struct{}
}

func NewAllowlist(authorities ...tool.Authority) Allowlist {
	allowed := make(map[tool.Authority]struct{}, len(authorities))
	for _, authority := range authorities {
		if authority.Valid() {
			allowed[authority] = struct{}{}
		}
	}

	return Allowlist{authorities: allowed}
}

func (a Allowlist) Decide(spec tool.Spec) Decision {
	if _, allowed := a.authorities[spec.Authority]; allowed {
		return DecisionAllow
	}

	return DecisionDeny
}
