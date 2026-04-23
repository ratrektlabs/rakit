package agent

import (
	"testing"

	"github.com/ratrektlabs/rakit/provider"
)

func TestRequireAllReturnsTrue(t *testing.T) {
	p := RequireAll()
	for _, name := range []string{"echo", "delete_item", ""} {
		if !p.Require(provider.ToolCall{Name: name}) {
			t.Errorf("RequireAll must require %q", name)
		}
	}
}

func TestRequireNoneReturnsFalse(t *testing.T) {
	p := RequireNone()
	if p.Require(provider.ToolCall{Name: "anything"}) {
		t.Fatal("RequireNone must never require")
	}
}

func TestRequireForMatchesNamesOnly(t *testing.T) {
	p := RequireFor("delete_item", "drop_table")
	if !p.Require(provider.ToolCall{Name: "delete_item"}) {
		t.Fatal("delete_item must require")
	}
	if !p.Require(provider.ToolCall{Name: "drop_table"}) {
		t.Fatal("drop_table must require")
	}
	if p.Require(provider.ToolCall{Name: "echo"}) {
		t.Fatal("echo must not require")
	}
	if p.Require(provider.ToolCall{Name: ""}) {
		t.Fatal("empty name must not require")
	}
}

func TestApprovalPolicyFuncAdapter(t *testing.T) {
	called := false
	p := ApprovalPolicyFunc(func(tc provider.ToolCall) bool {
		called = true
		return tc.Name == "x"
	})
	if !p.Require(provider.ToolCall{Name: "x"}) {
		t.Fatal("expected policy to require x")
	}
	if p.Require(provider.ToolCall{Name: "y"}) {
		t.Fatal("expected policy to pass y")
	}
	if !called {
		t.Fatal("adapter must delegate to underlying func")
	}
}
