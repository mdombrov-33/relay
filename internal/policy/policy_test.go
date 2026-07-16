package policy

import (
	"testing"

	"github.com/mdombrov-33/relay/internal/tool"
)

func TestAllowlistDecide(t *testing.T) {
	allowlist := NewAllowlist(tool.AuthorityRead)

	tests := []struct {
		name string
		spec tool.Spec
		want Decision
	}{
		{
			name: "allows listed authority",
			spec: tool.Spec{Authority: tool.AuthorityRead},
			want: DecisionAllow,
		},
		{
			name: "denies unlisted authority",
			spec: tool.Spec{Authority: tool.AuthorityEffect},
			want: DecisionDeny,
		},
		{
			name: "denies empty authority",
			spec: tool.Spec{},
			want: DecisionDeny,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allowlist.Decide(test.spec); got != test.want {
				t.Errorf("Decide(%q) = %q, want %q", test.spec.Authority, got, test.want)
			}
		})
	}
}

func TestAllowlistZeroValueDenies(t *testing.T) {
	var allowlist Allowlist

	if got := allowlist.Decide(tool.Spec{Authority: tool.AuthorityRead}); got != DecisionDeny {
		t.Errorf("zero Allowlist decision = %q, want %q", got, DecisionDeny)
	}
}
