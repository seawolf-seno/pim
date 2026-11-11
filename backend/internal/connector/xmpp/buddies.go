package xmpp

import (
	"context"
	"slices"

	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// XMPP's <show/> values (RFC 6121): "away", "xa" (extended away), "dnd"
// (do not disturb) and "chat" (free to chat); none means available.
const (
	showAway         = "away"
	showExtendedAway = "xa"
	showDoNotDisturb = "dnd"
	showChat         = "chat"
)

var _ connector.StatusSetter = (*Connector)(nil)

// buddyPresence is how available a contact last said it was: a
// domain.Presence value and its away message.
type buddyPresence struct {
	show   string
	status string
}

// presenceOf maps an XMPP presence onto a domain presence. ok is false
// for presence that says nothing about availability: subscription
// requests, probes and errors.
func presenceOf(p inPresence) (buddyPresence, bool) {
	switch p.Type {
	case "":
	case "unavailable":
		return buddyPresence{show: domain.PresenceOffline}, true
	default:
		return buddyPresence{}, false
	}

	switch p.Show {
	case showAway, showExtendedAway:
		return buddyPresence{show: domain.PresenceAway, status: p.Status}, true
	case showDoNotDisturb:
		return buddyPresence{show: domain.PresenceBusy, status: p.Status}, true
	default:
		return buddyPresence{show: domain.PresenceAvailable, status: p.Status}, true
	}
}

// onBuddyPresence records a roster contact's presence and reports the
// buddy. Presence from anyone not on the roster, or from the account's
// own other devices, is ignored.
func (c *Connector) onBuddyPresence(ctx context.Context, p inPresence) {
	presence, ok := presenceOf(p)
	from := bare(p.From)
	if !ok || from == "" || from == c.selfAddress() {
		return
	}

	c.mu.Lock()
	_, onRoster := c.names[from]
	if onRoster {
		c.presence[from] = presence
	}
	c.mu.Unlock()

	if onRoster {
		c.reportBuddy(ctx, from)
	}
}

// reportBuddy reports address's current groups and presence to the
// sink, when the sink keeps a buddy list. A contact no longer on the
// roster is reported offline with no groups.
func (c *Connector) reportBuddy(ctx context.Context, address string) {
	c.mu.Lock()
	reporter, ok := c.sink.(connector.BuddyReporter)
	name, onRoster := c.names[address]
	b := domain.Buddy{
		AccountID: c.account.ID, RemoteID: address, Name: name,
		Groups: slices.Clone(c.groups[address]), Show: domain.PresenceOffline,
	}
	if presence, known := c.presence[address]; known && onRoster {
		b.Show, b.Status = presence.show, presence.status
	}
	c.mu.Unlock()

	if ok {
		reporter.Buddy(ctx, b)
	}
}

// SetStatus sets the account's own availability and away message, and
// remembers them so a reconnect announces the same. A school server may
// still override them during class.
func (c *Connector) SetStatus(ctx context.Context, show, status string) error {
	c.mu.Lock()
	c.ownShow, c.ownStatus = show, status
	l := c.link
	c.mu.Unlock()
	if l == nil {
		return errNotConnected
	}

	return l.send(ctx, c.ownPresence())
}

// ownPresence is the account's own broadcast presence: available unless
// SetStatus said otherwise.
func (c *Connector) ownPresence() outPresence {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := outPresence{Status: c.ownStatus}
	switch c.ownShow {
	case domain.PresenceAway:
		p.Show = showAway
	case domain.PresenceBusy:
		p.Show = showDoNotDisturb
	}

	return p
}
