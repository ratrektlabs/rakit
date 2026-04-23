package agent

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rakit/tool"
)

type fakeClientTool struct{}

func (fakeClientTool) Name() string        { return "fake" }
func (fakeClientTool) Description() string { return "" }
func (fakeClientTool) Parameters() any     { return nil }
func (fakeClientTool) ClientSide() bool    { return true }
func (fakeClientTool) Execute(context.Context, map[string]any) (*tool.Result, error) {
	return tool.Ok("should not run"), nil
}

type fakeServerTool struct{}

func (fakeServerTool) Name() string        { return "fake_server" }
func (fakeServerTool) Description() string { return "" }
func (fakeServerTool) Parameters() any     { return nil }
func (fakeServerTool) Execute(context.Context, map[string]any) (*tool.Result, error) {
	return tool.Ok("ran"), nil
}

func TestIsClientSideMarker(t *testing.T) {
	if !isClientSide(fakeClientTool{}) {
		t.Fatal("ClientSide()=true must be detected")
	}
	if isClientSide(fakeServerTool{}) {
		t.Fatal("plain tool must not be flagged as client-side")
	}
}
