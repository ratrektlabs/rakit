package skill_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ratrektlabs/rakit/skill"
)

func TestHTTPToolExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%q want POST", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		// Echo the input under a known field
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": body,
			"meta":   "ignored",
		})
	}))
	defer srv.Close()

	tool := skill.NewHTTPTool(
		"echo",
		"echo",
		map[string]any{"type": "object"},
		srv.URL,
		map[string]string{"X-Test": "1"},
		nil,
		"result",
	)

	res, err := tool.Execute(context.Background(), map[string]any{"x": 1.0})
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("status=%q: %+v", res.Status, res)
	}

	// The response_field "result" should extract just the "result" key.
	m, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not a map: %T", res.Data)
	}
	if m["x"] != 1.0 {
		t.Fatalf("data=%+v", m)
	}
}

func TestHTTPToolHandlesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tool := skill.NewHTTPTool("t", "d", nil, srv.URL, nil, nil, "")
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("should not return raw error: %v", err)
	}
	if res.Status != "error" {
		t.Fatalf("want error result, got: %+v", res)
	}
	if !strings.Contains(res.Error, "500") {
		t.Fatalf("error should mention status code, got %q", res.Error)
	}
}

func TestToolFromDefRequiresEndpoint(t *testing.T) {
	_, err := skill.ToolFromDef(skill.ToolDef{Name: "t", Handler: "http"}, nil)
	if err == nil {
		t.Fatal("expected error for http handler without endpoint")
	}
}

func TestToolFromDefScriptRequiresPath(t *testing.T) {
	_, err := skill.ToolFromDef(skill.ToolDef{Name: "t", Handler: "script"}, nil)
	if err == nil {
		t.Fatal("expected error for script handler without script_path")
	}
}

func TestToolFromDefUnknownHandler(t *testing.T) {
	_, err := skill.ToolFromDef(skill.ToolDef{Name: "t", Handler: "ftp"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown handler")
	}
}
