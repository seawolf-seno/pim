package xmpp

import (
	"github.com/timlittle/omamessenger/backend/internal/connector"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// Provider implements connector.Provider for XMPP accounts.
type Provider struct{}

var _ connector.Provider = Provider{}

// Service reports the service id Provider handles.
func (Provider) Service() string { return domain.ServiceXMPP }

// Name is the human-readable name the UI shows for XMPP.
func (Provider) Name() string { return "XMPP" }

// Prepare checks and saves an account's address, password and, optionally,
// server and certificate file (see settingsFrom for the option names).
func (Provider) Prepare(dir, accountID string, options map[string]string) error {
	s, err := settingsFrom(options)
	if err != nil {
		return err
	}

	return saveSettings(dir, accountID, s)
}

// Connect returns the XMPP connector for the account, reading what Prepare
// saved from dir.
func (Provider) Connect(account domain.Account, dir string) connector.Connector {
	return New(account, dir)
}

// Forget deletes the account's saved settings from dir.
func (Provider) Forget(dir, accountID string) error {
	return forgetSettings(dir, accountID)
}
