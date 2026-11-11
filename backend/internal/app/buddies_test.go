package app_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/timlittle/omamessenger/backend/internal/app"
	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// TestBuddy_RecordsAndPublishes checks that a reported buddy is cleaned
// up, listed, and published, and that a later report replaces it.
func TestBuddy_RecordsAndPublishes(t *testing.T) {
	t.Parallel()

	f := newFixture(t, false)
	f.ingest.Buddy(t.Context(), domain.Buddy{
		AccountID: "x", RemoteID: "okafor@pim.test", Name: "Mr.\x00 Okafor",
		Groups: []string{"Biology", " ", "Biology", "Teachers"}, Show: domain.PresenceAway, Status: "  Teaching Biology · Block 2 ",
	})

	want := domain.Buddy{
		AccountID: "x", RemoteID: "okafor@pim.test", Name: "Mr. Okafor",
		Groups: []string{"Biology", "Teachers"}, Show: domain.PresenceAway, Status: "Teaching Biology · Block 2",
	}
	got, ok := f.published.lastOf(app.EventBuddyUpdated).(domain.Buddy)
	if !ok || !buddyEqual(got, want) {
		t.Errorf("published %+v, want %+v", got, want)
	}

	f.ingest.Buddy(t.Context(), domain.Buddy{AccountID: "x", RemoteID: "okafor@pim.test", Show: domain.PresenceAvailable})
	list := f.commands.Buddies(t.Context())
	if len(list) != 1 || list[0].Show != domain.PresenceAvailable || list[0].Name != "okafor@pim.test" {
		t.Errorf("Buddies = %+v, want the second report, named by address", list)
	}
}

// TestBuddy_Sanitizes checks the edges: no account or id is ignored, an
// unknown show is offline with no status, and groups are capped.
func TestBuddy_Sanitizes(t *testing.T) {
	t.Parallel()

	f := newFixture(t, false)
	f.ingest.Buddy(t.Context(), domain.Buddy{RemoteID: "a@pim.test"})
	f.ingest.Buddy(t.Context(), domain.Buddy{AccountID: "x"})
	if n := len(f.commands.Buddies(t.Context())); n != 0 {
		t.Fatalf("recorded %d buddies without an account or id", n)
	}

	groups := make([]string, 40)
	for i := range groups {
		groups[i] = fmt.Sprintf("g%d", i)
	}
	f.ingest.Buddy(t.Context(), domain.Buddy{AccountID: "x", RemoteID: "a@pim.test", Show: "chat", Status: "hi", Groups: groups})
	b := f.commands.Buddies(t.Context())[0]
	if b.Show != domain.PresenceOffline || b.Status != "" || len(b.Groups) != 32 {
		t.Errorf("buddy = %+v, want offline, no status, 32 groups", b)
	}
}

// TestBuddies_SortedAndOfflineWhenDisconnected checks the list's order,
// that a disconnect marks only that account's buddies offline (and
// publishes only the ones that changed), and that removing an account
// forgets its buddies.
func TestBuddies_SortedAndOfflineWhenDisconnected(t *testing.T) {
	t.Parallel()

	f := newFixture(t, false)
	for _, b := range []domain.Buddy{
		{AccountID: "wa", RemoteID: "z", Name: "Zed", Show: domain.PresenceAvailable},
		{AccountID: "wa", RemoteID: "a", Name: "Ama", Show: domain.PresenceBusy, Status: "Practice"},
		{AccountID: "wa", RemoteID: "o", Name: "Off", Show: domain.PresenceOffline},
		{AccountID: "x2", RemoteID: "k", Name: "Kenji", Show: domain.PresenceAvailable},
	} {
		f.ingest.Buddy(t.Context(), b)
	}
	if got := names(f.commands.Buddies(t.Context())); !slices.Equal(got, []string{"Ama", "Off", "Zed", "Kenji"}) {
		t.Errorf("order = %v", got)
	}

	f.published.take()
	f.ingest.AccountStatus(t.Context(), "wa", domain.AccountError, "lost")
	if got := f.published.take(); !slices.Equal(got, []string{app.EventAccountUpdated, app.EventBuddyUpdated, app.EventBuddyUpdated}) {
		t.Errorf("events = %v, want the account and its two buddies that were online", got)
	}
	for _, b := range f.commands.Buddies(t.Context()) {
		if (b.AccountID == "wa") != (b.Show == domain.PresenceOffline) || (b.AccountID == "wa" && b.Status != "") {
			t.Errorf("after the disconnect %+v", b)
		}
	}

	f.ingest.AccountStatus(t.Context(), "wa", domain.AccountConnected, "")
	if err := f.commands.RemoveAccount(t.Context(), "wa"); err != nil {
		t.Fatal(err)
	}
	if got := names(f.commands.Buddies(t.Context())); !slices.Equal(got, []string{"Kenji"}) {
		t.Errorf("after removing the account = %v", got)
	}
}

// TestSetStatus checks validation, the call through to the connector,
// and how its errors are reported.
func TestSetStatus(t *testing.T) {
	t.Parallel()

	f := newFixture(t, false)
	if err := f.commands.SetStatus(t.Context(), "wa", domain.PresenceAway, "  At practice\x00 "); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.status.set, []string{"wa away At practice"}) {
		t.Errorf("set = %v", f.status.set)
	}

	for _, tc := range []struct{ account, show string }{{"", domain.PresenceAway}, {"wa", domain.PresenceOffline}, {"wa", "chat"}} {
		if err := f.commands.SetStatus(t.Context(), tc.account, tc.show, ""); !errors.Is(err, app.ErrInvalidInput) {
			t.Errorf("SetStatus(%q, %q) = %v, want invalid input", tc.account, tc.show, err)
		}
	}

	for cause, wantInvalid := range map[error]bool{
		connector.ErrNoStatusSetter: true, connector.ErrNoConnector: true, errors.New("broken pipe"): false,
	} {
		f.status.err = cause
		err := f.commands.SetStatus(t.Context(), "wa", domain.PresenceBusy, "")
		if err == nil || errors.Is(err, app.ErrInvalidInput) != wantInvalid {
			t.Errorf("connector error %v became %v", cause, err)
		}
	}
}

// names lists buddies' names in order.
func names(buddies []domain.Buddy) []string {
	out := make([]string, len(buddies))
	for i, b := range buddies {
		out[i] = b.Name
	}

	return out
}

// buddyEqual compares two buddies, groups included.
func buddyEqual(a, b domain.Buddy) bool {
	return a.AccountID == b.AccountID && a.RemoteID == b.RemoteID && a.Name == b.Name &&
		a.Show == b.Show && a.Status == b.Status && strings.Join(a.Groups, "|") == strings.Join(b.Groups, "|")
}
