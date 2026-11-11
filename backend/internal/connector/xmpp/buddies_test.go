package xmpp

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/timlittle/omamessenger/backend/internal/connector/connectortest"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// TestBuddies_RosterThenPresence checks that the roster reports every
// contact offline with its groups, and that presence then moves each
// through away, busy, available and back offline.
func TestBuddies_RosterThenPresence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		okafor, ok := r.buddies.buddy("okafor@pim.test")
		if !ok || okafor.Show != domain.PresenceOffline || okafor.Name != "Mr. Okafor" ||
			!slices.Equal(okafor.Groups, []string{"Biology"}) || okafor.AccountID != "x1" {
			t.Fatalf("Mr. Okafor from the roster = %+v, %v", okafor, ok)
		}

		for _, tc := range []struct{ presence, show, status string }{
			{
				`<presence from="okafor@pim.test/laptop"><show>away</show><status>Teaching Biology · Block 2</status></presence>`,
				domain.PresenceAway, "Teaching Biology · Block 2",
			},
			{`<presence from="okafor@pim.test/laptop"><show>xa</show></presence>`, domain.PresenceAway, ""},
			{`<presence from="okafor@pim.test/laptop"><show>dnd</show><status>Grading</status></presence>`, domain.PresenceBusy, "Grading"},
			{`<presence from="okafor@pim.test/laptop"><show>chat</show></presence>`, domain.PresenceAvailable, ""},
			{`<presence from="okafor@pim.test/laptop"/>`, domain.PresenceAvailable, ""},
			{`<presence type="unavailable" from="okafor@pim.test/laptop"><status>bye</status></presence>`, domain.PresenceOffline, ""},
		} {
			r.push(tc.presence)
			got, _ := r.buddies.buddy("okafor@pim.test")
			if got.Show != tc.show || got.Status != tc.status || got.Name != "Mr. Okafor" {
				t.Errorf("after %s: %+v, want %s %q", tc.presence, got, tc.show, tc.status)
			}
		}
	})
}

// TestBuddies_IgnoredPresence checks the presence that reports no buddy:
// from someone not on the roster, from the account's own other device, a
// subscription request, and a room's.
func TestBuddies_IgnoredPresence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		before := r.buddies.count()
		r.push(`<presence from="stranger@elsewhere.test/x"/>`)
		r.push(`<presence from="rteague29@pim.test/phone"><show>away</show></presence>`)
		r.push(`<presence type="subscribe" from="okafor@pim.test"/>`)
		r.push(`<presence from=""/>`)
		r.push(selfPresence("biology@rooms.pim.test", "Rowan Teague"))

		if got := r.buddies.count(); got != before {
			t.Errorf("%d buddy reports from ignored presence", got-before)
		}
		if _, ok := r.buddies.buddy("stranger@elsewhere.test"); ok {
			t.Error("reported a stranger as a buddy")
		}
	})
}

// TestBuddies_RosterChanges checks that a roster push adds a buddy with its
// groups and that a removal reports it offline with no groups, forgetting
// its presence.
func TestBuddies_RosterChanges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(`<iq type="set" id="p1"><query xmlns="jabber:iq:roster">` +
			`<item jid="pell@pim.test" name="Dr. Pell" subscription="both"><group>Robotics Club</group><group>Teachers</group></item></query></iq>`)
		pell, _ := r.buddies.buddy("pell@pim.test")
		if !slices.Equal(pell.Groups, []string{"Robotics Club", "Teachers"}) || pell.Show != domain.PresenceOffline {
			t.Errorf("Dr. Pell = %+v", pell)
		}

		r.push(`<presence from="pell@pim.test/x"/>`)
		r.push(`<iq type="set" id="p2"><query xmlns="jabber:iq:roster"><item jid="pell@pim.test" subscription="remove"/></query></iq>`)
		pell, _ = r.buddies.buddy("pell@pim.test")
		if pell.Show != domain.PresenceOffline || len(pell.Groups) != 0 {
			t.Errorf("removed Dr. Pell = %+v, want offline with no groups", pell)
		}

		r.push(`<presence from="pell@pim.test/x"/>`)
		if pell, _ = r.buddies.buddy("pell@pim.test"); pell.Show != domain.PresenceOffline {
			t.Error("presence from a removed contact was reported")
		}
	})
}

// TestSetStatus checks the presence SetStatus sends, that it is announced
// again on the next connection, and that it fails while not connected.
func TestSetStatus(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fl := newFakeLink(schoolAnswers())
		r := startConnector(t, fl)
		for show, want := range map[string]string{
			domain.PresenceAway: "<show>away</show>", domain.PresenceBusy: "<show>dnd</show>",
		} {
			if err := r.c.SetStatus(t.Context(), show, "At practice"); err != nil {
				t.Fatal(err)
			}
			if len(fl.sentContaining("<presence", want, "<status>At practice</status>")) != 1 {
				t.Errorf("SetStatus(%s) sent %v", show, fl.sentXML())
			}
		}
		if err := r.c.SetStatus(t.Context(), domain.PresenceAway, "Back at 3"); err != nil {
			t.Fatal(err)
		}
		r.stop()

		again := newFakeLink(schoolAnswers())
		r.c.dial = func(context.Context, settings) (link, error) { return again, nil }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- r.c.Run(ctx, &connectortest.Sink{}) }()
		synctest.Wait()
		if len(again.sentContaining("<presence", "<show>away</show>", "<status>Back at 3</status>")) != 1 {
			t.Errorf("the reconnect announced %v", again.sentXML())
		}
		cancel()
		<-done
	})

	c := New(testAccount, t.TempDir())
	if err := c.SetStatus(t.Context(), domain.PresenceAway, ""); !errors.Is(err, errNotConnected) {
		t.Errorf("SetStatus while not connected = %v", err)
	}
}

// TestBuddies_WithoutABuddySink checks that a sink with no buddy list
// still gets contacts and nothing breaks.
func TestBuddies_WithoutABuddySink(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		if err := saveSettings(dir, testAccount.ID, settings{Address: "rteague29@pim.test", Password: "p"}); err != nil {
			t.Fatal(err)
		}
		c := New(testAccount, dir)
		fl := newFakeLink(schoolAnswers())
		c.dial = func(context.Context, settings) (link, error) { return fl, nil }
		sink := &connectortest.Sink{}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- c.Run(ctx, sink) }()
		synctest.Wait()

		fl.incoming <- `<presence from="okafor@pim.test/laptop"/>`
		synctest.Wait()
		if !sink.Has("contact okafor@pim.test Mr. Okafor") || !sink.Has("status x1 connected") {
			t.Errorf("lines = %v", sink.Lines())
		}
		cancel()
		<-done
	})
}
