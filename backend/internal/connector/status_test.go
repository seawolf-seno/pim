package connector_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/timlittle/omamessenger/backend/internal/connector"
)

func TestSetStatus_AsksConnectorsWithPresence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		chat := &withStatusSetter{fakeConnector: fakeConnector{id: "chat"}}
		m, _ := startManager(t, ctx, chat, &fakeConnector{id: "plain"})
		stopManager(t, cancel, m)

		if err := m.SetStatus(ctx, "chat", "away", "At practice"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(chat.statuses, []string{"away:At practice"}) {
			t.Errorf("statuses %v, want [away:At practice]", chat.statuses)
		}

		if err := m.SetStatus(ctx, "plain", "away", ""); !errors.Is(err, connector.ErrNoStatusSetter) {
			t.Errorf("SetStatus on a connector without presence = %v, want ErrNoStatusSetter", err)
		}
		if err := m.SetStatus(ctx, "nobody", "away", ""); !errors.Is(err, connector.ErrNoConnector) {
			t.Errorf("SetStatus for an unknown account = %v", err)
		}
	})
}
