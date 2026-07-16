package policy

import (
	"testing"

	"github.com/mdombrov-33/relay/internal/tool"
)

func TestAllowlistDecide(t *testing.T) {
	allowlist := NewAllowlist("lookup_customer", "lookup_incident")

	tests := []struct {
		name string
		call tool.Call
		want Decision
	}{
		{
			name: "allows listed tool",
			call: tool.Call{Name: "lookup_customer"},
			want: DecisionAllow,
		},
		{
			name: "denies unlisted tool",
			call: tool.Call{Name: "issue_credit"},
			want: DecisionDeny,
		},
		{
			name: "denies empty tool name",
			call: tool.Call{},
			want: DecisionDeny,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allowlist.Decide(test.call); got != test.want {
				t.Errorf("Decide(%q) = %q, want %q", test.call.Name, got, test.want)
			}
		})
	}
}

func TestAllowlistZeroValueDenies(t *testing.T) {
	var allowlist Allowlist

	if got := allowlist.Decide(tool.Call{Name: "lookup_customer"}); got != DecisionDeny {
		t.Errorf("zero Allowlist decision = %q, want %q", got, DecisionDeny)
	}
}
