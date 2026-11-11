package xmpp

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/timlittle/omamessenger/backend/internal/connector/connectortest"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// directMessage is Mr. Okafor writing to the account, archived by its
// server under archive-1, asking for a receipt.
const directMessage = `<message type="chat" id="m1" from="okafor@pim.test/laptop" to="rteague29@pim.test">` +
	`<body>Lab is due Friday.</body>` +
	`<stanza-id xmlns="urn:xmpp:sid:0" id="archive-1" by="rteague29@pim.test"/>` +
	`<request xmlns="urn:xmpp:receipts"/></message>`

// TestIncoming_DirectMessage checks a one-to-one message: its conversation
// is reported once, the message carries the archive id and the roster
// name, and a receipt goes back.
func TestIncoming_DirectMessage(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(directMessage)
		r.push(strings.Replace(directMessage, "archive-1", "archive-2", 1))

		conv, ok := r.sink.ConversationFor("okafor@pim.test")
		if !ok || conv.Kind != domain.KindDirect || conv.Title != "Mr. Okafor" {
			t.Errorf("conversation = %+v, %v", conv, ok)
		}
		if n := strings.Count(strings.Join(r.sink.Lines(), "\n"), "conversation okafor@pim.test"); n != 1 {
			t.Errorf("conversation reported %d times, want once", n)
		}
		live := r.sink.LiveMessages()["okafor@pim.test"]
		if len(live) != 2 {
			t.Fatalf("live = %+v", live)
		}
		connectortest.CheckIncoming(t, live[0])
		if live[0].RemoteID != "archive-1" || live[0].SenderName != "Mr. Okafor" || live[0].SenderID != "okafor@pim.test" {
			t.Errorf("message = %+v", live[0])
		}
		if got := r.link.sentContaining(`to="okafor@pim.test/laptop"`, `<received xmlns="urn:xmpp:receipts" id="m1"`); len(got) != 2 {
			t.Errorf("receipts sent = %v", r.link.sentXML())
		}
	})
}

// TestIncoming_DirectMessageEdges checks messages that are not reported:
// from the account itself, without a body; and that an unknown sender is
// named by their address and an offline message keeps its timestamp.
func TestIncoming_DirectMessageEdges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(`<message type="chat" from="rteague29@pim.test/phone"><body>from myself</body></message>`)
		r.push(`<message type="chat" from="okafor@pim.test"><composing xmlns="http://jabber.org/protocol/chatstates"/></message>`)
		r.push(`<message from="lvance29@pim.test/x" id="m9"><body>hi</body>` +
			`<delay xmlns="urn:xmpp:delay" stamp="2026-11-09T15:04:05Z"/></message>`)

		if len(r.sink.LiveMessages()["rteague29@pim.test"]) != 0 || len(r.sink.LiveMessages()["okafor@pim.test"]) != 0 {
			t.Errorf("reported a message that should be skipped: %v", r.sink.Lines())
		}
		live := r.sink.LiveMessages()["lvance29@pim.test"]
		if len(live) != 1 || live[0].SenderName != "lvance29@pim.test" || live[0].RemoteID != "m9" {
			t.Fatalf("live = %+v", live)
		}
		if live[0].Created != 1_794_236_645_000 {
			t.Errorf("Created = %d, want the delay stamp", live[0].Created)
		}
	})
}

// TestConformance_Duplicates checks that a message delivered twice carries
// the same remote id both times.
func TestConformance_Duplicates(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(directMessage)
		r.push(directMessage)

		live := r.sink.LiveMessages()["okafor@pim.test"]
		if len(live) != 2 {
			t.Fatalf("live = %+v", live)
		}
		connectortest.CheckDuplicates(t, live[0], live[1])
	})
}

// TestConformance_SendProgress checks a direct message going from sent to
// delivered when its receipt arrives, and a second receipt being ignored.
func TestConformance_SendProgress(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		conv := domain.Conversation{RemoteID: "okafor@pim.test", Kind: domain.KindDirect}
		if err := r.c.Send(t.Context(), conv, domain.Message{ID: "progress", Text: "hi"}); err != nil {
			t.Fatal(err)
		}
		id := r.sink.Outgoing("progress")[0].RemoteID

		receipt := `<message from="okafor@pim.test/laptop"><received xmlns="urn:xmpp:receipts" id="` + id + `"/></message>`
		r.push(receipt)
		r.push(receipt)

		connectortest.CheckSendProgress(t, r.sink, "progress")
		if updates := r.sink.Outgoing("progress"); len(updates) != 2 || updates[1].Status != domain.StatusDelivered {
			t.Errorf("updates = %+v, want sent then delivered once", updates)
		}
	})
}

// TestIncoming_ErrorReplyFailsTheMessage checks that a server refusing a
// sent message, as a school server does while class chat is closed, marks
// it failed; an error for an unknown id is ignored.
func TestIncoming_ErrorReplyFailsTheMessage(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		room := domain.Conversation{RemoteID: "biology@rooms.pim.test", Kind: domain.KindGroup}
		if err := r.c.Send(t.Context(), room, domain.Message{ID: "closed", Text: "can we start?"}); err != nil {
			t.Fatal(err)
		}
		id := r.sink.Outgoing("closed")[0].RemoteID

		r.push(`<message type="error" id="unknown" from="biology@rooms.pim.test"/>`)
		r.push(`<message type="error" id="` + id + `" from="biology@rooms.pim.test">` +
			`<error type="auth"><forbidden xmlns="` + nsStanzas + `"/>` +
			`<text xmlns="` + nsStanzas + `">Biology class chat is closed during class.</text></error></message>`)

		updates := r.sink.Outgoing("closed")
		if len(updates) != 2 || updates[1].Status != domain.StatusFailed {
			t.Errorf("updates = %+v, want sent then failed", updates)
		}
	})
}

// TestIncoming_RoomMessages checks a room: the self-presence names the
// account's nickname, the account's own echo marks its message delivered,
// others' messages arrive under their nickname, history on joining is
// history, and the room's own notices are shown as the room.
func TestIncoming_RoomMessages(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		const room = "biology@rooms.pim.test"
		r.push(selfPresence(room, "Rowan Teague"))
		r.push(`<message type="groupchat" from="` + room + `/Mr. Okafor"><body>Read 4.2 first.</body>` +
			`<delay xmlns="urn:xmpp:delay" stamp="2026-11-09T15:00:00Z"/>` +
			`<stanza-id xmlns="urn:xmpp:sid:0" id="h1" by="` + room + `"/></message>`)
		r.push(`<message type="groupchat" from="` + room + `"><body>Class chat is open for this period.</body></message>`)

		conv := domain.Conversation{RemoteID: room, Kind: domain.KindGroup}
		if err := r.c.Send(t.Context(), conv, domain.Message{ID: "mine", Text: "done"}); err != nil {
			t.Fatal(err)
		}
		id := r.sink.Outgoing("mine")[0].RemoteID
		r.push(`<message type="groupchat" from="` + room + `/Rowan Teague"><body>done</body>` +
			`<origin-id xmlns="urn:xmpp:sid:0" id="` + id + `"/></message>`)

		if u := r.sink.Outgoing("mine"); len(u) != 2 || u[1].Status != domain.StatusDelivered {
			t.Errorf("own message updates = %+v, want delivered", u)
		}
		history := r.sink.Messages()[room]
		if len(history) != 1 || history[0].RemoteID != "h1" || history[0].SenderName != "Mr. Okafor" {
			t.Errorf("history = %+v", history)
		}
		live := r.sink.LiveMessages()[room]
		if len(live) != 1 || live[0].SenderName != "Biology" || live[0].SenderID != room {
			t.Errorf("live = %+v, want only the room's notice, shown as the room", live)
		}
	})
}

// TestIncoming_OwnRoomMessageInHistory checks that the account's own room
// message seen again in history after a restart is reported as its own,
// under the id it was sent with, so the store matches it.
func TestIncoming_OwnRoomMessageInHistory(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		const room = "biology@rooms.pim.test"
		r.push(selfPresence(room, "Rowan Teague"))
		r.push(`<message type="groupchat" from="` + room + `/Rowan Teague"><body>earlier</body>` +
			`<origin-id xmlns="urn:xmpp:sid:0" id="sent-before"/>` +
			`<delay xmlns="urn:xmpp:delay" stamp="2026-11-09T14:00:00Z"/></message>`)

		history := r.sink.Messages()[room]
		if len(history) != 1 || !history[0].Outgoing || history[0].RemoteID != "sent-before" ||
			history[0].Status != domain.StatusDelivered {
			t.Errorf("history = %+v", history)
		}
	})
}

// TestIncoming_RoomEdges checks a message from a room not joined, and that
// leaving a room forgets the nickname so a later echo is not mistaken.
func TestIncoming_RoomEdges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(`<message type="groupchat" from="secret@rooms.pim.test/x"><body>hi</body></message>`)
		r.push(selfPresence("secret@rooms.pim.test", "Rowan Teague"))
		if len(r.sink.LiveMessages()["secret@rooms.pim.test"]) != 0 {
			t.Error("reported a message from a room not joined")
		}

		const room = "biology@rooms.pim.test"
		r.push(selfPresence(room, "Rowan Teague"))
		r.push(`<presence type="unavailable" from="` + room + `/Rowan Teague">` +
			`<x xmlns="` + nsMUCUser + `"><status code="110"/></x></presence>`)
		r.push(`<presence from="` + room + `/Mr. Okafor"><x xmlns="` + nsMUCUser + `"/></presence>`)
		r.push(`<message type="groupchat" from="` + room + `/Rowan Teague"><body>someone else now</body></message>`)

		live := r.sink.LiveMessages()[room]
		if len(live) != 1 || live[0].Outgoing {
			t.Errorf("live = %+v, want one message not taken as the account's own", live)
		}
	})
}

// TestIncoming_ServerRequests checks the replies to a roster change, a
// ping and an unsupported request, and that a roster change from anyone
// but the server is refused.
func TestIncoming_ServerRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(`<iq type="set" id="push1"><query xmlns="jabber:iq:roster">` +
			`<item jid="pell@pim.test" name="Dr. Pell" subscription="both"/></query></iq>`)
		r.push(`<iq type="set" id="push2" from="pim.test"><query xmlns="jabber:iq:roster">` +
			`<item jid="okafor@pim.test" subscription="remove"/></query></iq>`)
		r.push(`<iq type="set" id="spoof" from="kosei30@pim.test/x"><query xmlns="jabber:iq:roster">` +
			`<item jid="evil@elsewhere.test" name="Evil"/></query></iq>`)
		r.push(`<iq type="get" id="ping1" from="pim.test"><ping xmlns="urn:xmpp:ping"/></iq>`)
		r.push(`<iq type="get" id="v1" from="pim.test"><query xmlns="jabber:iq:version"/></iq>`)
		r.push(`<iq type="result" id="ignored"/>`)
		r.push(`<message type="chat" from="okafor@pim.test"><body>still here</body></message>`)

		if !r.sink.Has("contact pell@pim.test Dr. Pell") || r.sink.Has("contact evil@elsewhere.test Evil") {
			t.Errorf("contacts = %v", r.sink.Lines())
		}
		live := r.sink.LiveMessages()["okafor@pim.test"]
		if len(live) != 1 || live[0].SenderName != "okafor@pim.test" {
			t.Errorf("a removed contact should lose their roster name: %+v", live)
		}
		for id, want := range map[string]string{
			"push1": `type="result"`, "push2": `type="result"`, "ping1": `type="result"`,
			"spoof": "service-unavailable", "v1": "service-unavailable",
		} {
			if len(r.link.sentContaining(`id="`+id+`"`, want)) != 1 {
				t.Errorf("reply to %s: want %s in %v", id, want, r.link.sentXML())
			}
		}
		if len(r.link.sentContaining(`id="ignored"`)) != 0 {
			t.Error("replied to a result")
		}
	})
}

// TestIncoming_MalformedStanzaIsSkipped checks that a stanza that doesn't
// decode leaves the connection running.
func TestIncoming_MalformedStanzaIsSkipped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := startConnector(t, newFakeLink(schoolAnswers()))
		r.push(`<message type="chat" from="okafor@pim.test"><body>unterminated`)
		r.push(`<presence from="x"><x xmlns="` + nsMUCUser + `"><status code="nope"/></x></presence>`)
		r.push(`<iq type="get" id="bad"><query xmlns="jabber:iq:roster"><item jid="a"><group>x</iq>`)
		r.push(`<message type="chat" from="okafor@pim.test"><body>fine</body></message>`)

		if len(r.sink.LiveMessages()["okafor@pim.test"]) != 1 {
			t.Errorf("messages after malformed stanzas = %v", r.sink.Lines())
		}
	})
}
