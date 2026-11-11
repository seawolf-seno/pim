package server

import (
	"context"

	"github.com/timlittle/omamessenger/backend/internal/app"
)

// buddyMethods is the buddy list's entry: the buddies of every account
// whose service has presence, and the account's own away message.
func buddyMethods(c *app.Commands) map[string]method {
	return map[string]method{
		"buddies.list": bind(buddiesList(c)),
		"presence.set": bind(presenceSet(c)),
	}
}

// buddiesList returns every account's buddies.
func buddiesList(c *app.Commands) func(context.Context, none) (any, error) {
	return func(ctx context.Context, _ none) (any, error) {
		return c.Buddies(ctx), nil
	}
}

// presenceParams is an account's own availability and away message.
type presenceParams struct {
	AccountID string `json:"accountId"`
	Show      string `json:"show"`
	Status    string `json:"status"`
}

// presenceSet sets an account's own availability and away message.
func presenceSet(c *app.Commands) func(context.Context, presenceParams) (any, error) {
	return func(ctx context.Context, p presenceParams) (any, error) {
		return struct{}{}, c.SetStatus(ctx, p.AccountID, p.Show, p.Status)
	}
}
