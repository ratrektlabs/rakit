package agent

import "testing"

func TestRequireNone(t *testing.T) {
	p := RequireNone()
	if p.RequiresApproval("anything", `{}`) {
		t.Fatal("RequireNone must never require approval")
	}
}

func TestRequireAll(t *testing.T) {
	p := RequireAll()
	if !p.RequiresApproval("x", ``) {
		t.Fatal("RequireAll must always require approval")
	}
}

func TestRequireFor(t *testing.T) {
	p := RequireFor("delete_item", "charge_card")
	if !p.RequiresApproval("delete_item", `{}`) {
		t.Fatal("RequireFor must gate listed tools")
	}
	if p.RequiresApproval("echo", `{}`) {
		t.Fatal("RequireFor must not gate other tools")
	}
	// Case-sensitive comparison is part of the contract.
	if p.RequiresApproval("Delete_Item", `{}`) {
		t.Fatal("RequireFor must be case-sensitive")
	}
}

func TestApprovalPolicyFuncImplementsInterface(t *testing.T) {
	var p ApprovalPolicy = ApprovalPolicyFunc(func(name, _ string) bool {
		return name == "gated"
	})
	if !p.RequiresApproval("gated", "") {
		t.Fatal("func adapter did not route matched name")
	}
	if p.RequiresApproval("open", "") {
		t.Fatal("func adapter gated the wrong name")
	}
}

func TestInterruptKindMetadata(t *testing.T) {
	intr := Interrupt{Metadata: map[string]any{"rakit.kind": kindApproval}}
	if interruptKind(intr) != kindApproval {
		t.Fatalf("interruptKind=%q", interruptKind(intr))
	}
	if interruptKind(Interrupt{}) != "" {
		t.Fatal("empty metadata should yield empty kind")
	}
}

func TestAsApprovalPayload(t *testing.T) {
	for _, tc := range []struct {
		in       any
		wantVal  bool
		wantOK   bool
		describe string
	}{
		{true, true, true, "raw true"},
		{false, false, true, "raw false"},
		{map[string]any{"approved": true}, true, true, "map approved=true"},
		{map[string]any{"approved": false}, false, true, "map approved=false"},
		{map[string]any{"approved": "yes"}, false, false, "wrong type"},
		{map[string]any{}, false, false, "missing key"},
		{nil, false, false, "nil"},
	} {
		v, ok := asApprovalPayload(tc.in)
		if v != tc.wantVal || ok != tc.wantOK {
			t.Fatalf("%s: got (%v,%v) want (%v,%v)", tc.describe, v, ok, tc.wantVal, tc.wantOK)
		}
	}
}

func TestClientSidePayloadJSON(t *testing.T) {
	// An "output" key is unwrapped.
	got := clientSidePayloadJSON(map[string]any{"output": map[string]any{"x": 1}})
	if got != `{"x":1}` {
		t.Fatalf("output unwrap=%q", got)
	}
	// No "output" key — whole payload is serialised.
	got = clientSidePayloadJSON(map[string]any{"a": "b"})
	if got != `{"a":"b"}` {
		t.Fatalf("verbatim=%q", got)
	}
}
