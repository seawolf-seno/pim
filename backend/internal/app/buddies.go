package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// maxBuddyGroups bounds how many groups one buddy can be listed under: a
// roster is the service's (and so, ultimately, someone else's) data.
const maxBuddyGroups = 32

// StatusSetter sets an account's own availability and away message,
// through its connector (connector.Manager.SetStatus).
type StatusSetter interface {
	SetStatus(ctx context.Context, accountID, show, status string) error
}

// Ingest keeps the buddy list for connectors whose service has presence.
var _ connector.BuddyReporter = (*Ingest)(nil)

// buddyList holds every account's buddies in memory, by account and then
// by the buddy's remote id. Presence means nothing once an account
// disconnects, so nothing here is stored.
type buddyList struct {
	mu        sync.Mutex
	byAccount map[string]map[string]domain.Buddy
}

// newBuddyList returns an empty buddy list.
func newBuddyList() *buddyList {
	return &buddyList{byAccount: map[string]map[string]domain.Buddy{}}
}

// set records b, replacing what was recorded for it before.
func (l *buddyList) set(b domain.Buddy) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.byAccount[b.AccountID] == nil {
		l.byAccount[b.AccountID] = map[string]domain.Buddy{}
	}
	l.byAccount[b.AccountID][b.RemoteID] = b
}

// offline marks every buddy of accountID offline and returns the ones
// that changed.
func (l *buddyList) offline(accountID string) []domain.Buddy {
	l.mu.Lock()
	defer l.mu.Unlock()

	var changed []domain.Buddy
	for id, b := range l.byAccount[accountID] {
		if b.Show == domain.PresenceOffline && b.Status == "" {
			continue
		}
		b.Show, b.Status = domain.PresenceOffline, ""
		l.byAccount[accountID][id] = b
		changed = append(changed, b)
	}
	sortBuddies(changed)

	return changed
}

// forget drops every buddy of accountID.
func (l *buddyList) forget(accountID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.byAccount, accountID)
}

// all returns every buddy, sorted by account and then name.
func (l *buddyList) all() []domain.Buddy {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := []domain.Buddy{}
	for _, buddies := range l.byAccount {
		for _, b := range buddies {
			out = append(out, b)
		}
	}
	sortBuddies(out)

	return out
}

// sortBuddies orders buddies by account, name and remote id, so a list
// reads the same every time.
func sortBuddies(buddies []domain.Buddy) {
	slices.SortFunc(buddies, func(a, b domain.Buddy) int {
		return cmp.Or(cmp.Compare(a.AccountID, b.AccountID), cmp.Compare(a.Name, b.Name), cmp.Compare(a.RemoteID, b.RemoteID))
	})
}

// sanitizeBuddy caps a reported buddy's sender-controlled text, drops
// empty and repeated groups, and treats an unknown show as offline.
func sanitizeBuddy(b domain.Buddy) domain.Buddy {
	b.Name = domain.CapDisplayName(b.Name)
	if b.Name == "" {
		b.Name = b.RemoteID
	}
	b.Status = strings.TrimSpace(domain.CapDisplayName(b.Status))

	groups := []string{}
	for _, g := range b.Groups {
		g = strings.TrimSpace(domain.CapDisplayName(g))
		if g != "" && !slices.Contains(groups, g) && len(groups) < maxBuddyGroups {
			groups = append(groups, g)
		}
	}
	b.Groups = groups

	switch b.Show {
	case domain.PresenceAvailable, domain.PresenceAway, domain.PresenceBusy:
	default:
		b.Show, b.Status = domain.PresenceOffline, ""
	}

	return b
}

// Buddy records a buddy's groups and presence and publishes it. It makes
// Ingest a connector.BuddyReporter.
func (in *Ingest) Buddy(ctx context.Context, b domain.Buddy) {
	if b.AccountID == "" || b.RemoteID == "" {
		return
	}
	b = sanitizeBuddy(b)
	in.buddies.set(b)
	in.events.publish(ctx, EventBuddyUpdated, b)
}

// buddiesOffline marks an account's buddies offline once it is no longer
// connected, publishing each one that changed.
func (in *Ingest) buddiesOffline(ctx context.Context, accountID string) {
	for _, b := range in.buddies.offline(accountID) {
		in.events.publish(ctx, EventBuddyUpdated, b)
	}
}

// Buddies returns every account's buddies, for the buddy list.
func (c *Commands) Buddies(context.Context) []domain.Buddy {
	return c.buddies.all()
}

// SetStatus sets an account's own availability (available, away or busy)
// and away message ("" for none), through its connector.
func (c *Commands) SetStatus(ctx context.Context, accountID, show, status string) error {
	if accountID == "" {
		return fmt.Errorf("%w: accountId is required", ErrInvalidInput)
	}
	switch show {
	case domain.PresenceAvailable, domain.PresenceAway, domain.PresenceBusy:
	default:
		return fmt.Errorf("%w: show must be available, away or busy", ErrInvalidInput)
	}

	status = strings.TrimSpace(domain.CapDisplayName(status))
	err := c.status.SetStatus(ctx, accountID, show, status)
	switch {
	case errors.Is(err, connector.ErrNoStatusSetter):
		return fmt.Errorf("%w: this account's service has no away messages", ErrInvalidInput)
	case errors.Is(err, connector.ErrNoConnector):
		return fmt.Errorf("%w: no such account", ErrInvalidInput)
	case err != nil:
		return fmt.Errorf("app: set status: %w", err)
	}

	return nil
}
