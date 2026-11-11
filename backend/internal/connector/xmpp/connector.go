// Package xmpp connects an XMPP account, such as one on a school's own
// chat server, to the application: its roster becomes contacts, its rooms
// become group conversations, and one-to-one chats become direct ones.
//
// A conversation's remote id is the other side's bare address: a person
// ("rteague29@school.org") or a room ("biology@rooms.school.org").
package xmpp

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// requestTimeout bounds each request the connector waits on, so a server
// that stops answering fails the run instead of hanging it.
const requestTimeout = 20 * time.Second

// signInFailedDetail is the account status shown when the server refuses
// the address or password.
const signInFailedDetail = "The server didn't accept the address or password. Remove the account and add it again."

// errAlreadyRunning reports a second Run while one is in progress.
var errAlreadyRunning = errors.New("xmpp: already running")

// errConnectionClosed reports a server that ended the connection without
// an error, which still ends the run so the supervisor reconnects.
var errConnectionClosed = errors.New("xmpp: the server closed the connection")

// errNotConnected reports a send while the account is not connected.
var errNotConnected = errors.New("xmpp: not connected")

// Connector serves one XMPP account.
type Connector struct {
	account domain.Account
	dir     string
	dial    dialFunc
	running atomic.Bool

	// mu guards the fields below, which belong to the current run.
	mu      sync.Mutex
	link    link
	sink    connector.Sink
	self    string            // the account's own bare address
	names   map[string]string // roster names by bare address
	rooms   map[string]string // joined rooms' titles by room address
	nicks   map[string]string // the account's own nickname in each room
	direct  map[string]bool   // direct conversations already reported
	pending map[string]string // stanza id of a sent message -> its local id
}

var (
	_ connector.Connector         = (*Connector)(nil)
	_ connector.AttachmentLimiter = (*Connector)(nil)
)

// New returns a connector for account, whose settings Provider.Prepare
// saved in dir.
func New(account domain.Account, dir string) *Connector {
	return &Connector{account: account, dir: dir, dial: dialMellium}
}

// Account describes the account this connector serves.
func (c *Connector) Account() domain.Account { return c.account }

// AttachmentLimit is zero for every kind: this connector sends text only,
// so the composer offers no attachments for it.
func (*Connector) AttachmentLimit(string) int64 { return 0 }

// Run signs in and reports the account's contacts, rooms and messages to
// sink until ctx is cancelled or the connection fails. A refused password
// is reported as needing new settings and not retried.
func (c *Connector) Run(ctx context.Context, sink connector.Sink) error {
	if !c.running.CompareAndSwap(false, true) {
		return errAlreadyRunning
	}
	defer c.running.Store(false)

	id := c.account.ID
	sink.AccountStatus(ctx, id, domain.AccountConnecting, "")
	s, err := loadSettings(c.dir, id)
	if err != nil {
		return err
	}

	l, err := c.dial(ctx, s)
	if errors.Is(err, errSignIn) {
		sink.AccountStatus(ctx, id, domain.AccountNeedsAuth, signInFailedDetail)
		<-ctx.Done()

		return nil
	}
	if err != nil {
		return err
	}

	return c.serve(ctx, sink, l, bare(s.Address))
}

// serve runs one signed-in link: it reads stanzas in the background,
// fetches the roster and joins the rooms, then waits for the run to end.
func (c *Connector) serve(ctx context.Context, sink connector.Sink, l link, self string) error {
	c.begin(l, sink, self)
	defer c.end()

	served := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Go(func() {
		served <- l.serve(func(d *xml.Decoder, start xml.StartElement) error {
			return c.handle(ctx, d, start)
		})
	})

	err := c.start(ctx, l)
	if err == nil {
		sink.AccountStatus(ctx, c.account.ID, domain.AccountConnected, "")
		select {
		case <-ctx.Done():
		case err = <-served:
			if err == nil {
				err = errConnectionClosed
			}
		}
	}

	closeErr := l.close()
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}

	return errors.Join(err, closeErr)
}

// begin records a new run's link and sink, with empty state.
func (c *Connector) begin(l link, sink connector.Sink, self string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.link, c.sink, c.self = l, sink, self
	c.names = map[string]string{}
	c.rooms = map[string]string{}
	c.nicks = map[string]string{}
	c.direct = map[string]bool{}
	c.pending = map[string]string{}
}

// end forgets the run's link and sink, so Send reports not connected.
func (c *Connector) end() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.link, c.sink = nil, nil
}

// start fetches the roster, announces the account as available, and joins
// its rooms.
func (c *Connector) start(ctx context.Context, l link) error {
	if err := c.fetchRoster(ctx, l); err != nil {
		return err
	}
	if err := l.send(ctx, outPresence{}); err != nil {
		return err
	}

	return c.joinRooms(ctx, l)
}

// fetchRoster reports every roster contact.
func (c *Connector) fetchRoster(ctx context.Context, l link) error {
	var roster rosterQuery
	if err := request(ctx, l, query(newID(), "", nsRoster), &roster); err != nil {
		return fmt.Errorf("xmpp: roster: %w", err)
	}
	c.addContacts(ctx, roster.Items)

	return nil
}

// request is l.request bounded by requestTimeout.
func request(ctx context.Context, l link, iq outIQ, result any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return l.request(ctx, iq, result)
}

// addContacts records roster items' names and reports them as contacts. An
// item being removed from the roster only forgets its name: the store keeps
// the contact for its existing conversations.
func (c *Connector) addContacts(ctx context.Context, items []rosterItem) {
	for _, item := range items {
		address := bare(item.JID)
		if address == "" {
			continue
		}
		name := item.Name
		if name == "" {
			name = address
		}

		c.mu.Lock()
		if item.Subscription == "remove" {
			delete(c.names, address)
		} else {
			c.names[address] = name
		}
		sink := c.sink
		c.mu.Unlock()

		if item.Subscription != "remove" && sink != nil {
			sink.Contact(ctx, domain.Contact{AccountID: c.account.ID, RemoteID: address, Name: name})
		}
	}
}

// Send delivers an outgoing text message: to a room for a group
// conversation, as a chat message otherwise. It reports the message sent
// once written; a delivery receipt or the room's echo later marks it
// delivered, and an error reply marks it failed.
func (c *Connector) Send(ctx context.Context, conv domain.Conversation, m domain.Message) error {
	c.mu.Lock()
	l, sink := c.link, c.sink
	c.mu.Unlock()
	if l == nil {
		return errNotConnected
	}

	out := outMessage{ID: newID(), Type: typeChat, To: conv.RemoteID, Body: m.Text}
	out.OriginID = &stanzaIDTag{ID: out.ID}
	if conv.Kind == domain.KindGroup {
		out.Type = typeGroupchat
	} else {
		out.Request = &struct{}{}
	}

	c.mu.Lock()
	c.pending[out.ID] = m.ID
	c.mu.Unlock()
	if err := l.send(ctx, out); err != nil {
		c.takePending(out.ID)

		return err
	}
	sink.OutgoingStatus(ctx, m.ID, out.ID, domain.StatusSent)

	return nil
}

// MarkRead does nothing: this connector sends no read markers, so reading
// a conversation here tells no one.
func (*Connector) MarkRead(context.Context, domain.Conversation) error { return nil }

// takePending removes and returns the local id of the sent message with
// stanza id, or "" when it is not one this run is waiting on.
func (c *Connector) takePending(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	local := c.pending[id]
	delete(c.pending, id)

	return local
}
