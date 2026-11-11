package xmpp

import (
	"context"
	"fmt"

	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// joinRooms finds the server's room service, then joins every room it
// lists for this account and reports each as a group conversation. A
// server with no room service is not an error: the account just has no
// rooms. A school server lists only the rooms the account belongs to.
func (c *Connector) joinRooms(ctx context.Context, l link) error {
	service, err := c.roomService(ctx, l)
	if err != nil || service == "" {
		return err
	}

	var rooms discoItems
	if err := request(ctx, l, query(newID(), service, nsDiscoItem), &rooms); err != nil {
		return fmt.Errorf("xmpp: rooms: %w", err)
	}

	for _, item := range rooms.Items {
		if err := c.joinRoom(ctx, l, bare(item.JID), item.Name); err != nil {
			return err
		}
	}

	return nil
}

// roomService is the address of the server's multi-user chat service, or
// "" when it has none.
func (c *Connector) roomService(ctx context.Context, l link) (string, error) {
	var items discoItems
	if err := request(ctx, l, query(newID(), domainOf(c.selfAddress()), nsDiscoItem), &items); err != nil {
		return "", fmt.Errorf("xmpp: services: %w", err)
	}

	for _, item := range items.Items {
		var info discoInfo
		if err := request(ctx, l, query(newID(), item.JID, nsDiscoInfo), &info); err != nil {
			continue // one service not answering doesn't hide the others
		}
		if info.isRoomService() {
			return item.JID, nil
		}
	}

	return "", nil
}

// joinRoom reports room as a group conversation and asks to join it under
// the account's own name (its address's local part). The room's
// self-presence later says which nickname it really gave.
func (c *Connector) joinRoom(ctx context.Context, l link, room, title string) error {
	if room == "" {
		return nil
	}
	if title == "" {
		title = room
	}

	c.mu.Lock()
	c.rooms[room] = title
	sink := c.sink
	c.mu.Unlock()

	sink.Conversation(ctx, domain.Conversation{
		AccountID: c.account.ID, Service: domain.ServiceXMPP, RemoteID: room,
		Kind: domain.KindGroup, Title: title,
	})

	return l.send(ctx, joinPresence(room, localOf(c.selfAddress())))
}

// selfAddress is the account's own bare address in this run.
func (c *Connector) selfAddress() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.self
}

// localOf is the local part of a bare address: "a" in "a@b".
func localOf(address string) string {
	for i := range len(address) {
		if address[i] == '@' {
			return address[:i]
		}
	}

	return address
}

// domainOf is the domain of a bare address: "b" in "a@b".
func domainOf(address string) string {
	for i := range len(address) {
		if address[i] == '@' {
			return address[i+1:]
		}
	}

	return address
}
