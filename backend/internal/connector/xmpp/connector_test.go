package xmpp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/connector/connectortest"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// TestRun_ReportsConnectedWithContactsAndRooms checks a whole sign-in: the
// roster becomes contacts, the room service's rooms become conversations
// and are joined, and the account is announced available.
func TestRun_ReportsConnectedWithContactsAndRooms(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))

		for _, want := range []string{
			"status x1 connecting",
			"contact okafor@pim.test Mr. Okafor",
			"contact kosei30@pim.test kosei30@pim.test",
			"conversation biology@rooms.pim.test Biology",
			"status x1 connected",
		} {
			if !r.sink.Has(want) {
				t.Errorf("missing %q in %v", want, r.sink.Lines())
			}
		}
		conv, _ := r.sink.ConversationFor("biology@rooms.pim.test")
		if conv.Kind != domain.KindGroup || conv.Service != domain.ServiceXMPP {
			t.Errorf("room conversation = %+v, want a group on xmpp", conv)
		}
		if got := r.link.sentContaining(`to="biology@rooms.pim.test/rteague29"`, nsMUC, `maxstanzas="50"`); len(got) != 1 {
			t.Errorf("join presence sent %d times, want 1: %v", len(got), r.link.sentXML())
		}
		if got := r.link.sentContaining("<presence"); len(got) < 2 {
			t.Errorf("want an initial presence and a join, sent %v", r.link.sentXML())
		}
	})
}

// TestRun_WithoutRoomService checks that a server with no room service
// still connects, with no rooms.
func TestRun_WithoutRoomService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		answers := schoolAnswers()
		delete(answers, nsDiscoInfo+" rooms.pim.test")
		r := startConnector(t, newFakeLink(answers))

		if !r.sink.Has("status x1 connected") {
			t.Errorf("not connected: %v", r.sink.Lines())
		}
		if _, ok := r.sink.ConversationFor("biology@rooms.pim.test"); ok {
			t.Error("reported a room from a server without a room service")
		}
	})
}

// TestRun_FailsWhenARequestFails checks that a refused roster or room list
// ends the run with an error, so the supervisor retries.
func TestRun_FailsWhenARequestFails(t *testing.T) {
	t.Parallel()

	for name, key := range map[string]string{
		"roster":   nsRoster + " ",
		"services": nsDiscoItem + " pim.test",
		"rooms":    nsDiscoItem + " rooms.pim.test",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				answers := schoolAnswers()
				answers[key] = "!internal-server-error"
				r := startConnector(t, newFakeLink(answers))

				err := <-r.done
				r.done <- err // let the cleanup's stop read it again
				var refused errorIQ
				if !errors.As(err, &refused) || refused.condition != "internal-server-error" {
					t.Errorf("Run = %v, want the refused request's condition", err)
				}
				if r.sink.Has("status x1 connected") {
					t.Error("reported connected after a failed start")
				}
			})
		})
	}
}

// TestRun_EndsWhenTheServerCloses checks that a connection the server
// closes ends the run with an error, so the supervisor reconnects.
func TestRun_EndsWhenTheServerCloses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		if err := r.link.close(); err != nil {
			t.Fatal(err)
		}

		err := <-r.done
		r.done <- err
		if !errors.Is(err, errConnectionClosed) {
			t.Errorf("Run = %v, want errConnectionClosed", err)
		}
	})
}

// TestRun_SignInRefusedAsksForNewSettings checks that a refused password
// is reported as needing new settings and not retried.
func TestRun_SignInRefusedAsksForNewSettings(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		if err := saveSettings(dir, testAccount.ID, settings{Address: "rteague29@pim.test", Password: "wrong"}); err != nil {
			t.Fatal(err)
		}
		c := New(testAccount, dir)
		dials := 0
		c.dial = func(context.Context, settings) (link, error) {
			dials++

			return nil, signInError(errors.New("not-authorized"))
		}

		ctx, cancel := context.WithCancel(t.Context())
		sink := &connectortest.Sink{}
		done := make(chan error, 1)
		go func() { done <- c.Run(ctx, sink) }()
		synctest.Wait()

		if !sink.Has("status x1 needs-auth " + signInFailedDetail) {
			t.Errorf("want needs-auth, got %v", sink.Lines())
		}
		cancel()
		if err := <-done; err != nil || dials != 1 {
			t.Errorf("Run = %v after %d dials, want nil after 1", err, dials)
		}
	})
}

// TestRun_ReturnsDialAndSettingsErrors checks the two ways Run fails
// before it has a connection.
func TestRun_ReturnsDialAndSettingsErrors(t *testing.T) {
	t.Parallel()

	c := New(testAccount, t.TempDir())
	if err := c.Run(t.Context(), &connectortest.Sink{}); err == nil {
		t.Error("Run without saved settings succeeded")
	}

	dir := t.TempDir()
	if err := saveSettings(dir, testAccount.ID, settings{Address: "a@b.test", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	c = New(testAccount, dir)
	unreachable := errors.New("connection refused")
	c.dial = func(context.Context, settings) (link, error) { return nil, unreachable }
	if err := c.Run(t.Context(), &connectortest.Sink{}); !errors.Is(err, unreachable) {
		t.Errorf("Run = %v, want the dial error", err)
	}
}

// TestConformance_Lifecycle runs the shared lifecycle checks.
func TestConformance_Lifecycle(t *testing.T) {
	t.Parallel()

	connectortest.CheckLifecycle(t, func(t *testing.T) connector.Connector {
		t.Helper()
		dir := t.TempDir()
		if err := saveSettings(dir, testAccount.ID, settings{Address: "rteague29@pim.test", Password: "p"}); err != nil {
			t.Fatal(err)
		}
		c := New(testAccount, dir)
		c.dial = func(context.Context, settings) (link, error) { return newFakeLink(schoolAnswers()), nil }

		return c
	})
}

// TestSend_ChatAndRoomMessages checks what Send writes for a direct and a
// group conversation, and that each is reported sent with its stanza id.
func TestSend_ChatAndRoomMessages(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		direct := domain.Conversation{RemoteID: "okafor@pim.test", Kind: domain.KindDirect}
		room := domain.Conversation{RemoteID: "biology@rooms.pim.test", Kind: domain.KindGroup}

		if err := r.c.Send(t.Context(), direct, domain.Message{ID: "local-1", Text: "Is the lab due Friday?"}); err != nil {
			t.Fatal(err)
		}
		if err := r.c.Send(t.Context(), room, domain.Message{ID: "local-2", Text: "done with 4.2"}); err != nil {
			t.Fatal(err)
		}

		chat := r.link.sentContaining(`type="chat"`, `to="okafor@pim.test"`, "Is the lab due Friday?", nsReceipts)
		group := r.link.sentContaining(`type="groupchat"`, `to="biology@rooms.pim.test"`, "done with 4.2")
		if len(chat) != 1 || len(group) != 1 {
			t.Fatalf("sent %v", r.link.sentXML())
		}
		if strings.Contains(group[0], nsReceipts) {
			t.Error("a room message asked for a delivery receipt")
		}
		for _, local := range []string{"local-1", "local-2"} {
			updates := r.sink.Outgoing(local)
			if len(updates) != 1 || updates[0].Status != domain.StatusSent || updates[0].RemoteID == "" {
				t.Errorf("%s updates = %+v, want sent with a remote id", local, updates)
			}
		}
	})
}

// TestSend_Failures checks that Send reports a write that fails and a send
// while not connected, and records nothing as sent.
func TestSend_Failures(t *testing.T) {
	t.Parallel()

	c := New(testAccount, t.TempDir())
	conv := domain.Conversation{RemoteID: "okafor@pim.test"}
	if err := c.Send(t.Context(), conv, domain.Message{ID: "m", Text: "hi"}); !errors.Is(err, errNotConnected) {
		t.Errorf("Send while not connected = %v, want errNotConnected", err)
	}

	synctest.Test(t, func(t *testing.T) {
		fl := newFakeLink(schoolAnswers())
		r := startConnector(t, fl)
		fl.sendErr = errors.New("broken pipe")

		if err := r.c.Send(t.Context(), conv, domain.Message{ID: "m", Text: "hi"}); err == nil {
			t.Error("Send succeeded over a broken link")
		}
		if len(r.sink.Outgoing("m")) != 0 {
			t.Error("a failed write was reported sent")
		}
	})
}

// TestConnector_TextOnlyAndSilentRead checks the two small capabilities:
// no attachments, and reading tells no one.
func TestConnector_TextOnlyAndSilentRead(t *testing.T) {
	t.Parallel()

	c := New(testAccount, t.TempDir())
	if c.Account().ID != "x1" {
		t.Errorf("Account = %+v", c.Account())
	}
	for _, kind := range []string{"photo", "video", "file"} {
		if c.AttachmentLimit(kind) != 0 {
			t.Errorf("AttachmentLimit(%s) != 0", kind)
		}
	}
	if err := c.MarkRead(t.Context(), domain.Conversation{}); err != nil {
		t.Error(err)
	}
}
