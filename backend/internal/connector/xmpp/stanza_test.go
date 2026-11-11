package xmpp

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

// TestStanzaHelpers checks the small address and id helpers.
func TestStanzaHelpers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, bare, resource, local, domain string }{
		{"a@b/c", "a@b", "c", "a", "b"},
		{"a@b", "a@b", "", "a", "b"},
		{"room@rooms.b/Mr. Okafor/x", "room@rooms.b", "Mr. Okafor/x", "room", "rooms.b"},
		{"b", "b", "", "b", "b"},
	} {
		if got := bare(tc.in); got != tc.bare {
			t.Errorf("bare(%q) = %q", tc.in, got)
		}
		if got := resource(tc.in); got != tc.resource {
			t.Errorf("resource(%q) = %q", tc.in, got)
		}
		if got := localOf(tc.bare); got != tc.local {
			t.Errorf("localOf(%q) = %q", tc.bare, got)
		}
		if got := domainOf(tc.bare); got != tc.domain {
			t.Errorf("domainOf(%q) = %q", tc.bare, got)
		}
	}

	a, b := newID(), newID()
	if len(a) != 24 || a == b {
		t.Errorf("newID = %q, %q; want two different 24-character ids", a, b)
	}
}

// TestStamp checks timestamps from a delay, a bad delay and none.
func TestStamp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		m    inMessage
		want int64
	}{
		{"delay", inMessage{Delay: &delay{Stamp: "2026-11-09T15:04:05.5Z"}}, 1_794_236_645_500},
		{"bad delay", inMessage{Delay: &delay{Stamp: "yesterday"}}, now.UnixMilli()},
		{"none", inMessage{}, now.UnixMilli()},
	} {
		if got := stamp(tc.m, now); got != tc.want {
			t.Errorf("%s: stamp = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestIDs checks which id a message is known by.
func TestIDs(t *testing.T) {
	t.Parallel()

	m := inMessage{ID: "own", StanzaIDs: []stanzaIDTag{{ID: "other", By: "x@y"}, {ID: "mine", By: "a@b"}}}
	if got := archiveID(m, "a@b"); got != "mine" {
		t.Errorf("archiveID = %q", got)
	}
	if got := archiveID(m, "nobody"); got != "" {
		t.Errorf("archiveID for an unknown archive = %q", got)
	}
	if got := originID(m); got != "own" {
		t.Errorf("originID without origin-id = %q", got)
	}
	m.OriginID = &stanzaIDTag{ID: "origin"}
	if got := originID(m); got != "origin" {
		t.Errorf("originID = %q", got)
	}
	if got := remoteID(m, "nobody"); got != "origin" {
		t.Errorf("remoteID without an archive id = %q", got)
	}
}

// TestStanzaError checks reading an error's condition and text.
func TestStanzaError(t *testing.T) {
	t.Parallel()

	var m inMessage
	raw := `<message type="error"><error type="auth"><forbidden xmlns="` + nsStanzas + `"/>` +
		`<text xmlns="` + nsStanzas + `">closed</text><app xmlns="urn:example"/></error></message>`
	if err := xml.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.Error.Type != "auth" || m.Error.condition() != "forbidden" || m.Error.Text != "closed" {
		t.Errorf("error = %+v", m.Error)
	}

	var e stanzaError
	if err := xml.Unmarshal([]byte(`<error><app xmlns="urn:example"/></error>`), &e); err != nil || e.condition() != "undefined-condition" {
		t.Errorf("error without a condition = %+v, %v", e, err)
	}
	if err := xml.Unmarshal([]byte(`<error><forbidden>`), &e); err == nil {
		t.Error("decoded an unterminated error")
	}
	if err := xml.Unmarshal([]byte(`<error><text xmlns="`+nsStanzas+`"><b></text></error>`), &e); err == nil {
		t.Error("decoded a malformed text")
	}
}

// TestOutgoingStanzas checks the XML the connector writes.
func TestOutgoingStanzas(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		v    any
		want []string
	}{
		"roster request": {query("q1", "", nsRoster), []string{`<iq xmlns="jabber:client" id="q1" type="get">`, `<query xmlns="jabber:iq:roster">`}},
		"join":           {joinPresence("room@rooms.b", "rowan"), []string{`to="room@rooms.b/rowan"`, `<x xmlns="` + nsMUC + `"><history maxstanzas="50">`}},
		"error reply": {
			iqReply{ID: "i", Type: "error", Error: &errorReply{Type: "cancel", Condition: "service-unavailable"}},
			[]string{`<error type="cancel"><service-unavailable xmlns="` + nsStanzas + `"></service-unavailable></error>`},
		},
	} {
		data, err := xml.Marshal(tc.v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, w := range tc.want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s: %s lacks %s", name, data, w)
			}
		}
	}
}

// TestDiscoInfo_IsRoomService checks recognising a room service.
func TestDiscoInfo_IsRoomService(t *testing.T) {
	t.Parallel()

	var rooms, upload discoInfo
	if err := xml.Unmarshal([]byte(roomsInfoAnswer), &rooms); err != nil || !rooms.isRoomService() {
		t.Errorf("rooms = %+v, %v", rooms, err)
	}
	if err := xml.Unmarshal([]byte(uploadInfoAnswer), &upload); err != nil || upload.isRoomService() {
		t.Errorf("upload = %+v, %v", upload, err)
	}
}
