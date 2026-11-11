//go:build xmpplive

package xmpp

// A live test against a real XMPP server: the pim-server development server
// (https://github.com/seawolf-seno/pim-server), with its fictional school
// and the class clock pinned to Monday during Biology:
//
//	PIM_FAKE_NOW="2026-11-09 10:00" bin/pim-dev up        # in pim-server
//	PIM_CA=$PWD/.dev/certs/pim.test.crt                     # its certificate
//	go test -tags xmpplive -run Live ./backend/internal/connector/xmpp/
//
// It is not part of make check: it needs that server running. It waits on
// a real network, so it polls with a deadline instead of using synctest.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/timlittle/omamessenger/backend/internal/connector/connectortest"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// liveTimeout bounds each wait on the live server.
const liveTimeout = 10 * time.Second

// liveAccount runs a real connector for a fictional person and returns its
// sink once it is connected.
func liveAccount(t *testing.T, login string) (*Connector, *connectortest.Sink) {
	t.Helper()

	ca := os.Getenv("PIM_CA")
	if ca == "" {
		t.Skip("PIM_CA is not set; see this file's comment")
	}
	dir := t.TempDir()
	s := settings{Address: login + "@pim.test", Password: "pim-dev", Server: "127.0.0.1:5222", CAFile: ca}
	if err := saveSettings(dir, login, s); err != nil {
		t.Fatal(err)
	}

	c := New(domain.Account{ID: login, Service: domain.ServiceXMPP}, dir)
	sink := &connectortest.Sink{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, sink) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("%s: Run = %v", login, err)
		}
	})

	eventually(t, login+" connected", func() bool { return sink.Has("status " + login + " connected") })

	return c, sink
}

// eventually polls check until it holds or liveTimeout passes.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(liveTimeout)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLive_SchoolServer signs in a student and a teacher and checks
// contacts, rooms, room messages, the class-chat rule and direct messages.
func TestLive_SchoolServer(t *testing.T) {
	rowan, rowanSink := liveAccount(t, "rteague29")
	okafor, okaforSink := liveAccount(t, "okafor")
	const room = "biology@rooms.pim.test"

	if !rowanSink.Has("contact okafor@pim.test Mr. Okafor") {
		t.Errorf("Rowan's contacts: %v", rowanSink.Lines())
	}
	if conv, ok := rowanSink.ConversationFor(room); !ok || conv.Title != "Biology" {
		t.Errorf("Rowan's Biology room = %+v, %v", conv, ok)
	}
	eventually(t, "Mr. Okafor's room nickname", func() bool {
		okafor.mu.Lock()
		defer okafor.mu.Unlock()

		return okafor.nicks[room] == "Mr. Okafor"
	})

	group := domain.Conversation{RemoteID: room, Kind: domain.KindGroup}
	if err := okafor.Send(t.Context(), group, domain.Message{ID: "t1", Text: "Read 4.2 first."}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the teacher's post delivered", func() bool { return lastStatus(okaforSink, "t1") == domain.StatusDelivered })
	eventually(t, "Rowan gets the teacher's post", func() bool { return hasLive(rowanSink, room, "Read 4.2 first.", "Mr. Okafor") })

	if err := rowan.Send(t.Context(), group, domain.Message{ID: "s1", Text: "can we start?"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the student's post refused during class", func() bool { return lastStatus(rowanSink, "s1") == domain.StatusFailed })

	direct := domain.Conversation{RemoteID: "okafor@pim.test", Kind: domain.KindDirect}
	if err := rowan.Send(t.Context(), direct, domain.Message{ID: "d1", Text: "Is the lab due Friday?"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Mr. Okafor gets the direct message", func() bool {
		return hasLive(okaforSink, "rteague29@pim.test", "Is the lab due Friday?", "Rowan Teague")
	})
	eventually(t, "the direct message delivered", func() bool { return lastStatus(rowanSink, "d1") == domain.StatusDelivered })
}

// lastStatus is the latest delivery status recorded for a sent message.
func lastStatus(sink *connectortest.Sink, local string) string {
	updates := sink.Outgoing(local)
	if len(updates) == 0 {
		return ""
	}

	return updates[len(updates)-1].Status
}

// hasLive reports whether conversation remote has a live message with text
// from sender.
func hasLive(sink *connectortest.Sink, remote, text, sender string) bool {
	for _, m := range sink.LiveMessages()[remote] {
		if m.Text == text && m.SenderName == sender {
			return true
		}
	}

	return false
}
