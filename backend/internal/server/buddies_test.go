package server_test

import (
	"testing"

	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// TestBuddies_ListAndSetPresence checks buddies.list and presence.set
// through the protocol, including a refused show.
func TestBuddies_ListAndSetPresence(t *testing.T) {
	t.Parallel()

	s := connect(t, false)
	if got, err := call[[]domain.Buddy](t, s, "buddies.list", nil); err != nil || len(got) != 0 {
		t.Fatalf("buddies.list before any = %+v, %v", got, err)
	}

	s.ingest.Buddy(t.Context(), domain.Buddy{
		AccountID: "wa", RemoteID: "okafor@pim.test", Name: "Mr. Okafor",
		Groups: []string{"Biology"}, Show: domain.PresenceAway, Status: "Teaching Biology · Block 2",
	})
	got, err := call[[]domain.Buddy](t, s, "buddies.list", nil)
	if err != nil || len(got) != 1 || got[0].Status != "Teaching Biology · Block 2" || got[0].Groups[0] != "Biology" {
		t.Errorf("buddies.list = %+v, %v", got, err)
	}

	params := map[string]string{"accountId": "wa", "show": "away", "status": "At practice"}
	if _, err := call[struct{}](t, s, "presence.set", params); err != nil {
		t.Errorf("presence.set = %v", err)
	}
	params["show"] = "offline"
	if _, err := call[struct{}](t, s, "presence.set", params); code(err) != -32602 {
		t.Errorf("presence.set offline = %v, want invalid params", err)
	}
}
