package xmpp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"strings"
	"time"
)

// XML namespaces the connector reads or writes.
const (
	nsClient    = "jabber:client"
	nsRoster    = "jabber:iq:roster"
	nsDiscoInfo = "http://jabber.org/protocol/disco#info"
	nsDiscoItem = "http://jabber.org/protocol/disco#items"
	nsMUC       = "http://jabber.org/protocol/muc"
	nsMUCUser   = "http://jabber.org/protocol/muc#user"
	nsDelay     = "urn:xmpp:delay"
	nsStanzaID  = "urn:xmpp:sid:0"
	nsReceipts  = "urn:xmpp:receipts"
	nsPing      = "urn:xmpp:ping"
	nsStanzas   = "urn:ietf:params:xml:ns:xmpp-stanzas"
)

// Message types (RFC 6121 and XEP-0045).
const (
	typeChat      = "chat"
	typeGroupchat = "groupchat"
	typeNormal    = "normal"
	typeError     = "error"
)

// mucSelfPresence is the XEP-0045 status code a room puts on the presence
// that describes the receiving user themself, which carries their real
// nickname in the room.
const mucSelfPresence = 110

// historyOnJoin is how many recent room messages to ask for when joining a
// room, so a room opened after a restart still shows its latest
// conversation.
const historyOnJoin = 50

// inMessage is a <message/> as it arrives from the server.
type inMessage struct {
	ID        string        `xml:"id,attr"`
	Type      string        `xml:"type,attr"`
	From      string        `xml:"from,attr"`
	Body      string        `xml:"body"`
	Delay     *delay        `xml:"urn:xmpp:delay delay"`
	StanzaIDs []stanzaIDTag `xml:"urn:xmpp:sid:0 stanza-id"`
	OriginID  *stanzaIDTag  `xml:"urn:xmpp:sid:0 origin-id"`
	Request   *struct{}     `xml:"urn:xmpp:receipts request"`
	Received  *receiptTag   `xml:"urn:xmpp:receipts received"`
	Error     *stanzaError  `xml:"error"`
}

// delay is XEP-0203's timestamp on a message delivered late: from
// offline storage or a room's history.
type delay struct {
	Stamp string `xml:"stamp,attr"`
}

// stanzaIDTag is XEP-0359's stanza-id (assigned by the server or room that
// archived the message, named in By) or origin-id (assigned by the sender).
type stanzaIDTag struct {
	ID string `xml:"id,attr"`
	By string `xml:"by,attr,omitempty"`
}

// receiptTag is XEP-0184's delivery receipt for the message with ID.
type receiptTag struct {
	ID string `xml:"id,attr"`
}

// stanzaError is the <error/> inside an error reply: a condition element
// in the stanzas namespace, and optional human-readable text. Its own
// UnmarshalXML reads it.
type stanzaError struct {
	Type     string
	Children []xml.Name
	Text     string
}

// UnmarshalXML keeps only the names of the error's child elements, which is
// all the condition needs, plus its text.
func (e *stanzaError) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		if a.Name.Local == "type" {
			e.Type = a.Value
		}
	}

	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := e.child(d, t); err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		}
	}
}

// child records one child of an <error/>: its text, or a condition name.
func (e *stanzaError) child(d *xml.Decoder, t xml.StartElement) error {
	if t.Name.Space == nsStanzas && t.Name.Local == "text" {
		return d.DecodeElement(&e.Text, &t)
	}
	e.Children = append(e.Children, t.Name)

	return d.Skip()
}

// condition is the error's defined condition, such as "forbidden".
func (e *stanzaError) condition() string {
	for _, n := range e.Children {
		if n.Space == nsStanzas {
			return n.Local
		}
	}

	return "undefined-condition"
}

// inPresence is a <presence/> as it arrives from the server.
type inPresence struct {
	Type   string   `xml:"type,attr"`
	From   string   `xml:"from,attr"`
	Show   string   `xml:"show"`
	Status string   `xml:"status"`
	User   *mucUser `xml:"http://jabber.org/protocol/muc#user x"`
}

// mucUser is XEP-0045's room-specific presence extension.
type mucUser struct {
	Statuses []struct {
		Code int `xml:"code,attr"`
	} `xml:"status"`
}

// isSelf reports whether a room presence describes the receiving user.
func (p inPresence) isSelf() bool {
	if p.User == nil {
		return false
	}
	for _, s := range p.User.Statuses {
		if s.Code == mucSelfPresence {
			return true
		}
	}

	return false
}

// inIQ is an <iq/> request the server sends the client: a roster push or a
// ping. Results of the client's own requests never reach the handler.
type inIQ struct {
	ID     string       `xml:"id,attr"`
	Type   string       `xml:"type,attr"`
	From   string       `xml:"from,attr"`
	Roster *rosterQuery `xml:"jabber:iq:roster query"`
	Ping   *struct{}    `xml:"urn:xmpp:ping ping"`
}

// rosterQuery is the roster (the buddy list), or a change to it.
type rosterQuery struct {
	XMLName xml.Name     `xml:"jabber:iq:roster query"`
	Items   []rosterItem `xml:"item"`
}

// rosterItem is one contact on the roster.
type rosterItem struct {
	JID          string   `xml:"jid,attr"`
	Name         string   `xml:"name,attr"`
	Subscription string   `xml:"subscription,attr"`
	Groups       []string `xml:"group"`
}

// discoItems is a XEP-0030 items list: a server's services, or a room
// service's rooms.
type discoItems struct {
	XMLName xml.Name `xml:"http://jabber.org/protocol/disco#items query"`
	Items   []struct {
		JID  string `xml:"jid,attr"`
		Name string `xml:"name,attr"`
	} `xml:"item"`
}

// discoInfo is a XEP-0030 info result: what an entity is.
type discoInfo struct {
	XMLName    xml.Name `xml:"http://jabber.org/protocol/disco#info query"`
	Identities []struct {
		Category string `xml:"category,attr"`
		Type     string `xml:"type,attr"`
	} `xml:"identity"`
}

// isRoomService reports whether info describes a multi-user chat service.
func (i discoInfo) isRoomService() bool {
	for _, id := range i.Identities {
		if id.Category == "conference" && id.Type == "text" {
			return true
		}
	}

	return false
}

// outIQ is an <iq/> request the client sends, with Payload as its child.
type outIQ struct {
	XMLName xml.Name `xml:"jabber:client iq"`
	ID      string   `xml:"id,attr"`
	Type    string   `xml:"type,attr"`
	To      string   `xml:"to,attr,omitempty"`
	Payload any
}

// emptyQuery is a request's empty <query/> in namespace NS, as roster and
// disco requests use.
type emptyQuery struct {
	XMLName xml.Name
}

// query builds an IQ get with an empty query in ns, addressed to to (or
// the user's own server when empty).
func query(id, to, ns string) outIQ {
	return outIQ{ID: id, Type: "get", To: to, Payload: emptyQuery{XMLName: xml.Name{Space: ns, Local: "query"}}}
}

// iqReply is the client's answer to a server's IQ request: a bare result,
// or an error with a condition.
type iqReply struct {
	XMLName xml.Name    `xml:"jabber:client iq"`
	ID      string      `xml:"id,attr"`
	Type    string      `xml:"type,attr"`
	To      string      `xml:"to,attr,omitempty"`
	Error   *errorReply `xml:"error,omitempty"`
}

// errorReply is the <error/> the client puts in a reply to a request it
// does not handle.
type errorReply struct {
	Type      string
	Condition string
}

// MarshalXML writes the error with its condition as an empty child.
func (e errorReply) MarshalXML(enc *xml.Encoder, start xml.StartElement) error {
	start.Attr = []xml.Attr{{Name: xml.Name{Local: "type"}, Value: e.Type}}
	cond := xml.StartElement{Name: xml.Name{Space: nsStanzas, Local: e.Condition}}
	for _, tok := range []xml.Token{start, cond, cond.End(), start.End()} {
		if err := enc.EncodeToken(tok); err != nil {
			return err
		}
	}

	return nil
}

// outMessage is a <message/> the client sends.
type outMessage struct {
	XMLName  xml.Name     `xml:"jabber:client message"`
	ID       string       `xml:"id,attr"`
	Type     string       `xml:"type,attr"`
	To       string       `xml:"to,attr"`
	Body     string       `xml:"body,omitempty"`
	OriginID *stanzaIDTag `xml:"urn:xmpp:sid:0 origin-id,omitempty"`
	Request  *struct{}    `xml:"urn:xmpp:receipts request,omitempty"`
	Received *receiptTag  `xml:"urn:xmpp:receipts received,omitempty"`
}

// outPresence is a <presence/> the client sends: its availability (with
// an optional show and away message), or a request to join a room.
type outPresence struct {
	XMLName xml.Name  `xml:"jabber:client presence"`
	To      string    `xml:"to,attr,omitempty"`
	Show    string    `xml:"show,omitempty"`
	Status  string    `xml:"status,omitempty"`
	Join    *joinRoom `xml:"http://jabber.org/protocol/muc x,omitempty"`
}

// joinRoom is XEP-0045's join request, asking for recent history.
type joinRoom struct {
	History struct {
		MaxStanzas int `xml:"maxstanzas,attr"`
	} `xml:"history"`
}

// joinPresence asks to join room under nick, with recent history. The
// server may give the user a different nickname (a school server gives
// everyone their own name); the room's self-presence says which.
func joinPresence(room, nick string) outPresence {
	j := &joinRoom{}
	j.History.MaxStanzas = historyOnJoin

	return outPresence{To: room + "/" + nick, Join: j}
}

// bare is address without its resource: "a@b/c" becomes "a@b".
func bare(address string) string {
	before, _, _ := strings.Cut(address, "/")

	return before
}

// resource is address's resource, or "" when it has none.
func resource(address string) string {
	_, after, _ := strings.Cut(address, "/")

	return after
}

// stamp is when m was sent, in Unix milliseconds: its delay timestamp when
// it has a valid one, or now.
func stamp(m inMessage, now time.Time) int64 {
	if m.Delay != nil {
		if t, err := time.Parse(time.RFC3339Nano, m.Delay.Stamp); err == nil {
			return t.UnixMilli()
		}
	}

	return now.UnixMilli()
}

// archiveID is the id the server or room (by) archived m under, or "".
func archiveID(m inMessage, by string) string {
	for _, s := range m.StanzaIDs {
		if s.By == by && s.ID != "" {
			return s.ID
		}
	}

	return ""
}

// originID is the id m's sender gave it: its origin-id, or else its own id.
func originID(m inMessage) string {
	if m.OriginID != nil && m.OriginID.ID != "" {
		return m.OriginID.ID
	}

	return m.ID
}

// newID makes a random stanza id. Ids are random rather than counters so
// that two runs, or two devices, never reuse one.
func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error (Go 1.24+)

	return hex.EncodeToString(b[:])
}
