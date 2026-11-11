package xmpp

import (
	"context"
	"encoding/xml"
	"log"
	"time"

	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// handle decodes one stanza the server sent and acts on it. A stanza that
// fails to decode is skipped, not fatal: one malformed stanza must not end
// the whole connection.
func (c *Connector) handle(ctx context.Context, d *xml.Decoder, start xml.StartElement) error {
	switch start.Name.Local {
	case "message":
		var m inMessage
		if err := d.DecodeElement(&m, &start); err != nil {
			log.Printf("xmpp: skipped a message that did not decode")

			break
		}
		c.onMessage(ctx, m)
	case "presence":
		var p inPresence
		if err := d.DecodeElement(&p, &start); err == nil {
			c.onPresence(p)
		}
	case "iq":
		var iq inIQ
		if err := d.DecodeElement(&iq, &start); err == nil {
			return c.onIQ(ctx, iq)
		}
	}

	return nil
}

// onMessage routes a message: an error reply or delivery receipt for one of
// ours, or a new message in a room or a direct chat.
func (c *Connector) onMessage(ctx context.Context, m inMessage) {
	switch {
	case m.Type == typeError:
		c.onError(ctx, m)
	case m.Received != nil:
		c.onReceipt(ctx, m.Received.ID)
	case m.Body == "":
		// Chat states, room subjects and other bodiless messages carry
		// nothing to show.
	case m.Type == typeGroupchat:
		c.onRoomMessage(ctx, m)
	default:
		c.onDirectMessage(ctx, m)
	}
}

// onError marks the sent message an error reply answers as failed, such as
// one a school server refused because class chat is closed.
func (c *Connector) onError(ctx context.Context, m inMessage) {
	local := c.takePending(m.ID)
	if local == "" {
		return
	}

	condition := "undefined-condition"
	if m.Error != nil {
		condition = m.Error.condition()
	}
	log.Printf("xmpp: a sent message was refused (condition=%s)", condition)
	c.currentSink().OutgoingStatus(ctx, local, m.ID, domain.StatusFailed)
}

// onReceipt marks the sent message a delivery receipt names as delivered.
func (c *Connector) onReceipt(ctx context.Context, id string) {
	if local := c.takePending(id); local != "" {
		c.currentSink().OutgoingStatus(ctx, local, id, domain.StatusDelivered)
	}
}

// onDirectMessage reports a one-to-one message, first reporting its
// conversation when this run hasn't yet, and sends a delivery receipt when
// the sender asked for one. Messages held for the account while it was
// offline arrive delayed, but they are new to the user, so they are
// reported as incoming like any other.
func (c *Connector) onDirectMessage(ctx context.Context, m inMessage) {
	from := bare(m.From)
	if from == "" || from == c.selfAddress() {
		return
	}
	name := c.directConversation(ctx, from)

	msg := domain.Message{
		RemoteID: remoteID(m, c.selfAddress()), SenderID: from, SenderName: name,
		Text: m.Body, Created: stamp(m, time.Now()), Status: domain.StatusReceived,
	}
	c.currentSink().Incoming(ctx, c.account.ID, from, msg)

	if m.Request != nil && m.ID != "" {
		c.sendReceipt(ctx, m.From, m.ID)
	}
}

// directConversation reports the direct conversation with address the
// first time this run sees it, and returns the name to show for them.
func (c *Connector) directConversation(ctx context.Context, address string) string {
	c.mu.Lock()
	name, known := c.names[address]
	if !known {
		name = address
	}
	seen := c.direct[address]
	c.direct[address] = true
	sink := c.sink
	c.mu.Unlock()

	if !seen {
		sink.Conversation(ctx, domain.Conversation{
			AccountID: c.account.ID, Service: domain.ServiceXMPP, RemoteID: address,
			Kind: domain.KindDirect, Title: name,
		})
	}

	return name
}

// sendReceipt tells the sender their message arrived (XEP-0184).
func (c *Connector) sendReceipt(ctx context.Context, to, id string) {
	c.mu.Lock()
	l := c.link
	c.mu.Unlock()
	if l == nil {
		return
	}

	receipt := outMessage{ID: newID(), To: to, Received: &receiptTag{ID: id}}
	if err := l.send(ctx, receipt); err != nil {
		log.Printf("xmpp: could not send a delivery receipt")
	}
}

// remoteID is the id to store a received message under: the id the
// account's archive (by) gave it when there is one, since the sender's own
// id is not guaranteed unique, or else the sender's.
func remoteID(m inMessage, by string) string {
	if id := archiveID(m, by); id != "" {
		return id
	}

	return originID(m)
}

// currentSink is the current run's sink. Stanzas are only handled during a
// run, so it is never nil when a handler calls it.
func (c *Connector) currentSink() connector.Sink {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sink
}

// onRoomMessage reports a message in a joined room. The account's own
// message coming back from the room is how a room confirms it was
// delivered; seen again in history after a restart, it is reported as the
// account's own so the store can match it. Room history sent on joining is
// reported as history, not as new messages.
func (c *Connector) onRoomMessage(ctx context.Context, m inMessage) {
	room, nick := bare(m.From), resource(m.From)
	c.mu.Lock()
	title, joined := c.rooms[room]
	own := nick != "" && nick == c.nicks[room]
	c.mu.Unlock()
	if !joined {
		return
	}

	if own {
		if local := c.takePending(originID(m)); local != "" {
			c.currentSink().OutgoingStatus(ctx, local, originID(m), domain.StatusDelivered)

			return
		}
	}

	msg := roomMessage(m, title, own)
	if m.Delay != nil {
		c.currentSink().History(ctx, c.account.ID, room, msg)

		return
	}
	c.currentSink().Incoming(ctx, c.account.ID, room, msg)
}

// roomMessage normalizes a room message. A sender is their nickname in the
// room; a message from the room itself, such as a school server's "class
// chat is open" notice, is shown as coming from the room.
func roomMessage(m inMessage, title string, own bool) domain.Message {
	room, nick := bare(m.From), resource(m.From)
	msg := domain.Message{
		RemoteID: remoteID(m, room), SenderID: m.From, SenderName: nick,
		Text: m.Body, Created: stamp(m, time.Now()), Status: domain.StatusReceived,
	}
	if nick == "" {
		msg.SenderID, msg.SenderName = room, title
	}
	if own {
		msg.RemoteID, msg.Outgoing, msg.Status = originID(m), true, domain.StatusDelivered
	}

	return msg
}

// onPresence records the account's own nickname in a room from the room's
// self-presence: a school server renames everyone to their school name, so
// the nickname asked for may not be the one given.
func (c *Connector) onPresence(p inPresence) {
	if !p.isSelf() {
		return
	}
	room, nick := bare(p.From), resource(p.From)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, joined := c.rooms[room]; !joined {
		return
	}
	if p.Type == "unavailable" {
		delete(c.nicks, room)

		return
	}
	c.nicks[room] = nick
}

// onIQ answers the requests a server sends a client: a roster change
// (applied, then acknowledged) and a ping. Anything else gets the standard
// "not supported" error, as XMPP requires every request to be answered.
func (c *Connector) onIQ(ctx context.Context, iq inIQ) error {
	if iq.Type != "get" && iq.Type != "set" {
		return nil
	}

	reply := iqReply{ID: iq.ID, Type: "result", To: iq.From}
	switch {
	case iq.Type == "set" && iq.Roster != nil && c.fromServer(iq.From):
		c.addContacts(ctx, iq.Roster.Items)
	case iq.Type == "get" && iq.Ping != nil:
	default:
		reply.Type = typeError
		reply.Error = &errorReply{Type: "cancel", Condition: "service-unavailable"}
	}

	c.mu.Lock()
	l := c.link
	c.mu.Unlock()
	if l == nil {
		return nil
	}

	return l.send(ctx, reply)
}

// fromServer reports whether a stanza's sender is the account's own server
// or account, the only senders a roster change may come from (RFC 6121).
func (c *Connector) fromServer(from string) bool {
	self := c.selfAddress()

	return from == "" || from == self || bare(from) == self || from == domainOf(self)
}
