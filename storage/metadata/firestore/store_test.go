package firestore

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

func TestToSessionMapUsesLowerCamelIDFields(t *testing.T) {
	t.Parallel()

	data := toSessionMap(&metadata.Session{
		AgentID:         "agent-1",
		UserID:          "user-1",
		ParentSessionID: "parent-1",
	})

	for key, want := range map[string]string{
		"agentId":         "agent-1",
		"userId":          "user-1",
		"parentSessionId": "parent-1",
	} {
		if got := data[key]; got != want {
			t.Fatalf("%s = %v, want %q", key, got, want)
		}
	}
	for _, key := range []string{"agentID", "userID", "parentSessionID"} {
		if _, ok := data[key]; ok {
			t.Fatalf("legacy field %q should not be written", key)
		}
	}
}

func TestSessionSummaryFromMapReadsLegacyIDFields(t *testing.T) {
	t.Parallel()

	sess, err := sessionSummaryFromMap("session-1", map[string]any{
		"agentID":         "agent-1",
		"userID":          "user-1",
		"parentSessionID": "parent-1",
		"createdAt":       int64(10),
		"updatedAt":       int64(20),
		"revision":        int64(3),
	})
	if err != nil {
		t.Fatalf("sessionSummaryFromMap: %v", err)
	}
	if sess.AgentID != "agent-1" || sess.UserID != "user-1" || sess.ParentSessionID != "parent-1" {
		t.Fatalf("legacy fields decoded incorrectly: %+v", sess)
	}
	if sess.Revision != 3 || sess.CreatedAt != 10 || sess.UpdatedAt != 20 {
		t.Fatalf("session metadata decoded incorrectly: %+v", sess)
	}
}

func TestSessionFirestoreTagsMatchJSONTags(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(metadata.Session{})
	for _, name := range []string{"AgentID", "UserID", "ParentSessionID"} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("metadata.Session.%s not found", name)
		}
		got := strings.Split(field.Tag.Get("firestore"), ",")[0]
		want := strings.Split(field.Tag.Get("json"), ",")[0]
		if got != want {
			t.Fatalf("metadata.Session.%s firestore tag %q does not match JSON tag %q", name, got, want)
		}
	}
}
