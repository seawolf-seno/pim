package xmpp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"mellium.im/xmpp/jid"

	"github.com/timlittle/omamessenger/backend/internal/connector"
)

// defaultPort is the standard XMPP client port, used when the server is
// given without one or not given at all.
const defaultPort = "5222"

// settings are what an account needs to sign in: its address, its
// password, and optionally where the server is and which certificate to
// trust. They are saved by Provider.Prepare, readable only by the owner,
// the same way the other services keep their sessions.
type settings struct {
	// Address is the account's bare address, such as "rteague29@pim.test".
	Address string `json:"address"`

	// Password signs the account in. The connector offers it only through
	// SASL over TLS.
	Password string `json:"password"`

	// Server is "host" or "host:port" to connect to instead of the
	// address's own domain, such as a school server on the local network.
	Server string `json:"server,omitempty"`

	// CAFile is a PEM file of certificates to trust instead of the
	// system's, for a school or development server with its own
	// certificate authority.
	CAFile string `json:"caFile,omitempty"`
}

// settingsFrom checks the options an account is added with: "address" and
// "password" are required; "server" and "caFile" are optional.
func settingsFrom(options map[string]string) (settings, error) {
	s := settings{
		Address:  strings.TrimSpace(options["address"]),
		Password: options["password"],
		Server:   strings.TrimSpace(options["server"]),
		CAFile:   strings.TrimSpace(options["caFile"]),
	}

	addr, err := jid.Parse(s.Address)
	if err != nil || addr.Localpart() == "" || addr.Resourcepart() != "" {
		return settings{}, fmt.Errorf("xmpp: %w: enter an address like name@school.org", connector.ErrInvalidSetup)
	}
	s.Address = addr.String()

	if s.Password == "" {
		return settings{}, fmt.Errorf("xmpp: %w: enter the account's password", connector.ErrInvalidSetup)
	}

	if s.Server != "" {
		if _, err := serverAddress(s); err != nil {
			return settings{}, fmt.Errorf("xmpp: %w: enter the server as host or host:port", connector.ErrInvalidSetup)
		}
	}

	if s.CAFile != "" && !filepath.IsAbs(s.CAFile) {
		return settings{}, fmt.Errorf("xmpp: %w: the certificate file needs a full path", connector.ErrInvalidSetup)
	}

	return s, nil
}

// serverAddress is the host:port to dial for s: its Server, with the
// default port added when it has none, or else its address's domain.
func serverAddress(s settings) (string, error) {
	if s.Server == "" {
		addr, err := jid.Parse(s.Address)
		if err != nil {
			return "", fmt.Errorf("xmpp: server address: %w", err)
		}

		return net.JoinHostPort(addr.Domainpart(), defaultPort), nil
	}

	host, port, err := net.SplitHostPort(s.Server)
	if err != nil {
		host, port = s.Server, defaultPort
	}
	if host == "" || strings.ContainsAny(host, "/ ") || port == "" {
		return "", errors.New("xmpp: server address: not a host or host:port")
	}

	return net.JoinHostPort(host, port), nil
}

// saveSettings writes an account's settings into dir, readable only by the
// owner.
func saveSettings(dir, accountID string, s settings) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("xmpp: save settings: %w", err)
	}

	// The password is saved on purpose: it is the account's sign-in, kept
	// like every service's session, in a file only the owner can read.
	data, err := json.Marshal(s) //nolint:gosec // the account's own sign-in, written 0600 in the private data directory
	if err != nil {
		return fmt.Errorf("xmpp: save settings: %w", err)
	}

	path := settingsPath(dir, accountID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("xmpp: save settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("xmpp: save settings: %w", err)
	}

	return nil
}

// loadSettings reads an account's settings from dir.
func loadSettings(dir, accountID string) (settings, error) {
	var s settings
	data, err := os.ReadFile(settingsPath(dir, accountID))
	if err != nil {
		return s, fmt.Errorf("xmpp: load settings: %w", err)
	}

	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("xmpp: load settings: %w", err)
	}

	return s, nil
}

// forgetSettings deletes an account's settings from dir. A file already
// gone is not an error: forgetting is idempotent.
func forgetSettings(dir, accountID string) error {
	err := os.Remove(settingsPath(dir, accountID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("xmpp: forget: %w", err)
	}

	return nil
}

// settingsPath is where an account's settings live inside dir.
func settingsPath(dir, accountID string) string {
	return filepath.Join(dir, accountID+"-xmpp.json")
}
