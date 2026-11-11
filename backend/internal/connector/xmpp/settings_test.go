package xmpp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/timlittle/omamessenger/backend/internal/connector"
)

// TestSettingsFrom checks which options an account can be added with.
func TestSettingsFrom(t *testing.T) {
	t.Parallel()

	ok := []struct {
		name    string
		options map[string]string
		want    settings
	}{
		{
			"address and password",
			map[string]string{"address": " rteague29@pim.test ", "password": "p"},
			settings{Address: "rteague29@pim.test", Password: "p"},
		},
		{"server and certificate", map[string]string{
			"address": "a@pim.test", "password": "p", "server": "127.0.0.1:5222", "caFile": "/etc/pim/ca.pem",
		}, settings{Address: "a@pim.test", Password: "p", Server: "127.0.0.1:5222", CAFile: "/etc/pim/ca.pem"}},
		{
			"server without port",
			map[string]string{"address": "a@pim.test", "password": "p", "server": "chat.school.org"},
			settings{Address: "a@pim.test", Password: "p", Server: "chat.school.org"},
		},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := settingsFrom(tc.options)
			if err != nil || got != tc.want {
				t.Errorf("settingsFrom = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}

	bad := map[string]map[string]string{
		"no address":         {"password": "p"},
		"no local part":      {"address": "pim.test", "password": "p"},
		"with resource":      {"address": "a@pim.test/phone", "password": "p"},
		"no password":        {"address": "a@pim.test"},
		"bad server":         {"address": "a@pim.test", "password": "p", "server": "has space:5222"},
		"empty port":         {"address": "a@pim.test", "password": "p", "server": "host:"},
		"relative ca":        {"address": "a@pim.test", "password": "p", "caFile": "ca.pem"},
		"not an address":     {"address": "@@", "password": "p"},
		"server with a path": {"address": "a@pim.test", "password": "p", "server": "host/x"},
	}
	for name, options := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := settingsFrom(options); !errors.Is(err, connector.ErrInvalidSetup) {
				t.Errorf("settingsFrom(%v) = %v, want ErrInvalidSetup", options, err)
			}
		})
	}
}

// TestServerAddress checks where the connector dials.
func TestServerAddress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		s    settings
		want string
	}{
		{settings{Address: "a@pim.test"}, "pim.test:5222"},
		{settings{Address: "a@pim.test", Server: "127.0.0.1"}, "127.0.0.1:5222"},
		{settings{Address: "a@pim.test", Server: "chat.school.org:5223"}, "chat.school.org:5223"},
		{settings{Address: "a@pim.test", Server: "[::1]:5222"}, "[::1]:5222"},
	} {
		if got, err := serverAddress(tc.s); err != nil || got != tc.want {
			t.Errorf("serverAddress(%+v) = %q, %v; want %q", tc.s, got, err, tc.want)
		}
	}
	if _, err := serverAddress(settings{Address: "@@"}); err == nil {
		t.Error("serverAddress of an invalid address succeeded")
	}
}

// TestSettingsFiles checks that settings are saved privately, read back,
// and forgotten, and that forgetting twice is fine.
func TestSettingsFiles(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "accounts")
	want := settings{Address: "a@pim.test", Password: "secret", Server: "127.0.0.1"}
	if err := saveSettings(dir, "acct", want); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(settingsPath(dir, "acct"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if dirInfo, _ := os.Stat(dir); dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %v, want 0700", dirInfo.Mode().Perm())
	}
	got, err := loadSettings(dir, "acct")
	if err != nil || got != want {
		t.Errorf("loadSettings = %+v, %v", got, err)
	}

	for range 2 {
		if err := forgetSettings(dir, "acct"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadSettings(dir, "acct"); err == nil {
		t.Error("settings still load after forgetting")
	}
}

// TestSettingsFiles_Failures checks unreadable and unwritable settings.
func TestSettingsFiles_Failures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(settingsPath(dir, "bad"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSettings(dir, "bad"); err == nil {
		t.Error("loaded corrupt settings")
	}

	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveSettings(filepath.Join(file, "sub"), "a", settings{}); err == nil {
		t.Error("saved settings under a file")
	}
	if err := os.Mkdir(settingsPath(dir, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsPath(dir, "dir"), "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveSettings(dir, "dir", settings{}); err == nil {
		t.Error("replaced a non-empty directory with settings")
	}
	if err := forgetSettings(dir, "dir"); err == nil {
		t.Error("forgot a non-empty directory")
	}
}

// TestProvider checks the provider end to end over a directory.
func TestProvider(t *testing.T) {
	t.Parallel()

	var p Provider
	if p.Service() != "xmpp" || p.Name() != "XMPP" {
		t.Errorf("provider = %q %q", p.Service(), p.Name())
	}

	dir := t.TempDir()
	if err := p.Prepare(dir, "acct", map[string]string{"address": "a@pim.test"}); !errors.Is(err, connector.ErrInvalidSetup) {
		t.Errorf("Prepare without a password = %v", err)
	}
	if err := p.Prepare(dir, "acct", map[string]string{"address": "a@pim.test", "password": "p"}); err != nil {
		t.Fatal(err)
	}
	if c := p.Connect(testAccount, dir); c.Account().ID != testAccount.ID {
		t.Errorf("Connect = %+v", c.Account())
	}
	if err := p.Forget(dir, "acct"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSettings(dir, "acct"); err == nil {
		t.Error("Forget left the settings")
	}
}
