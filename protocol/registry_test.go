package protocol_test

import (
	"context"
	"io"
	"testing"

	"github.com/ratrektlabs/rakit/protocol"
)

type fakeProto struct{ name string }

func (f *fakeProto) Name() string                               { return f.name }
func (f *fakeProto) ContentType() string                        { return "text/x-" + f.name }
func (f *fakeProto) Encode(w io.Writer, _ protocol.Event) error { return nil }
func (f *fakeProto) EncodeStream(_ context.Context, _ io.Writer, _ <-chan protocol.Event) error {
	return nil
}
func (f *fakeProto) Decode(_ io.Reader) (protocol.Event, error) { return nil, nil }
func (f *fakeProto) DecodeStream(_ context.Context, _ io.Reader) (<-chan protocol.Event, error) {
	return nil, nil
}

func TestRegistryNegotiate(t *testing.T) {
	agui := &fakeProto{name: "ag-ui"}
	aisdk := &fakeProto{name: "ai-sdk"}

	reg := protocol.NewRegistry()
	reg.Register(agui)
	reg.Register(aisdk)
	reg.SetDefault(aisdk)

	cases := []struct {
		accept string
		want   protocol.Protocol
	}{
		{"text/vnd.ag-ui", agui},
		{"application/vnd.ag-ui", agui},
		{"text/vnd.ai-sdk", aisdk},
		{"application/vnd.ai-sdk", aisdk},
		{"text/event-stream", aisdk}, // falls through to default
		{"text/vnd.ag-ui; q=0.8", agui},
		{"text/vnd.ai-sdk, text/vnd.ag-ui", aisdk}, // first wins
		{"TEXT/VND.AG-UI", agui},                   // case-insensitive
		{"application/json", nil},                  // unknown
		{"", nil},
	}

	for _, c := range cases {
		got := reg.Negotiate(c.accept)
		if got != c.want {
			t.Errorf("Negotiate(%q)=%v want %v", c.accept, got, c.want)
		}
	}
}

func TestRegistryGet(t *testing.T) {
	reg := protocol.NewRegistry()
	p := &fakeProto{name: "x"}
	reg.Register(p)
	if reg.Get("x") != p {
		t.Fatal("Get(x) did not return registered protocol")
	}
	if reg.Get("missing") != nil {
		t.Fatal("Get(missing) want nil")
	}
}
