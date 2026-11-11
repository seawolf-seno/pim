package xmpp

// The connector's tests are internal: they replace the connector's dial
// function with a fake link, which only this package can reach. Everything
// else they check goes through the connector's own methods and the Sink.

import (
	"context"
	"encoding/xml"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/timlittle/omamessenger/backend/internal/connector/connectortest"
	"github.com/timlittle/omamessenger/backend/internal/domain"
)

// testAccount is the fictional account the tests sign in as.
var testAccount = domain.Account{ID: "x1", Service: domain.ServiceXMPP, Name: "Rowan"}

// The fictional school server's answers to the connector's requests, keyed
// by the request's namespace and address. "!condition" answers with an
// error carrying that condition.
const (
	rosterAnswer = `<query xmlns="jabber:iq:roster">` +
		`<item jid="okafor@pim.test" name="Mr. Okafor" subscription="both"><group>Biology</group></item>` +
		`<item jid="kosei30@pim.test" subscription="both"/></query>`
	servicesAnswer = `<query xmlns="http://jabber.org/protocol/disco#items">` +
		`<item jid="upload.pim.test"/><item jid="rooms.pim.test"/></query>`
	uploadInfoAnswer = `<query xmlns="http://jabber.org/protocol/disco#info">` +
		`<identity category="store" type="file"/></query>`
	roomsInfoAnswer = `<query xmlns="http://jabber.org/protocol/disco#info">` +
		`<identity category="conference" type="text"/></query>`
	roomsAnswer = `<query xmlns="http://jabber.org/protocol/disco#items">` +
		`<item jid="biology@rooms.pim.test" name="Biology"/></query>`
)

// schoolAnswers is a server with a roster, an upload service and a room
// service holding one room.
func schoolAnswers() map[string]string {
	return map[string]string{
		nsRoster + " ":                   rosterAnswer,
		nsDiscoItem + " pim.test":        servicesAnswer,
		nsDiscoInfo + " upload.pim.test": uploadInfoAnswer,
		nsDiscoInfo + " rooms.pim.test":  roomsInfoAnswer,
		nsDiscoItem + " rooms.pim.test":  roomsAnswer,
	}
}

// fakeLink is a scripted XMPP server connection: requests get answers from
// a map, stanzas the test pushes reach the connector's handler, and every
// stanza the connector sends is recorded as XML.
type fakeLink struct {
	answers  map[string]string
	incoming chan string
	closed   chan struct{}
	once     sync.Once
	sendErr  error

	mu   sync.Mutex
	sent []string
}

// newFakeLink returns a link answering requests from answers.
func newFakeLink(answers map[string]string) *fakeLink {
	return &fakeLink{answers: answers, incoming: make(chan string), closed: make(chan struct{})}
}

// send records v as XML, or fails with sendErr.
func (f *fakeLink) send(_ context.Context, v any) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	data, err := xml.Marshal(v)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, string(data))

	return nil
}

// request answers iq from the script through the real decodeResult.
func (f *fakeLink) request(_ context.Context, iq outIQ, result any) error {
	q, _ := iq.Payload.(emptyQuery)
	answer, ok := f.answers[q.XMLName.Space+" "+iq.To]
	var raw string
	switch {
	case !ok:
		raw = `<iq type="error"><error type="cancel"><item-not-found xmlns="` + nsStanzas + `"/></error></iq>`
	case strings.HasPrefix(answer, "!"):
		raw = `<iq type="error"><error type="cancel"><` + answer[1:] + ` xmlns="` + nsStanzas + `"/></error></iq>`
	default:
		raw = `<iq type="result">` + answer + `</iq>`
	}

	return decodeResult(xml.NewDecoder(strings.NewReader(raw)), result)
}

// serve hands each pushed stanza to handle until the link closes.
func (f *fakeLink) serve(handle func(d *xml.Decoder, start xml.StartElement) error) error {
	for {
		select {
		case raw := <-f.incoming:
			// Hand the stanza over the way mellium does: its start element
			// apart, the rest as tokens, through the same stanzaDecoder.
			inner := xml.NewDecoder(strings.NewReader(raw))
			start, err := firstStart(inner)
			if err != nil {
				return err
			}
			d, err := stanzaDecoder(tokenStream{inner}, start)
			if err != nil {
				return err
			}
			if err := handle(d, start); err != nil {
				return err
			}
		case <-f.closed:
			return nil
		}
	}
}

// close ends serve.
func (f *fakeLink) close() error {
	f.once.Do(func() { close(f.closed) })

	return nil
}

// sentXML returns every stanza sent so far.
func (f *fakeLink) sentXML() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.sent...)
}

// sentContaining returns the sent stanzas that contain every part.
func (f *fakeLink) sentContaining(parts ...string) []string {
	var out []string
	for _, s := range f.sentXML() {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(s, p)
		}
		if all {
			out = append(out, s)
		}
	}

	return out
}

// running is a connector signed in over a fake link, inside a synctest
// bubble, with what it reported recorded in sink.
type running struct {
	c      *Connector
	link   *fakeLink
	sink   *connectortest.Sink
	cancel context.CancelFunc
	done   chan error
}

// startConnector saves settings for testAccount, runs a connector over
// link and waits until it is idle. Call it inside synctest.Test.
func startConnector(t *testing.T, fl *fakeLink) *running {
	t.Helper()

	dir := t.TempDir()
	if err := saveSettings(dir, testAccount.ID, settings{Address: "rteague29@pim.test", Password: "pim-dev"}); err != nil {
		t.Fatal(err)
	}
	c := New(testAccount, dir)
	c.dial = func(context.Context, settings) (link, error) { return fl, nil }

	ctx, cancel := context.WithCancel(t.Context())
	r := &running{c: c, link: fl, sink: &connectortest.Sink{}, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- c.Run(ctx, r.sink) }()
	synctest.Wait()
	t.Cleanup(r.stop)

	return r
}

// push delivers one stanza from the server and waits until it is handled.
func (r *running) push(raw string) {
	r.link.incoming <- raw
	synctest.Wait()
}

// stop cancels the run and waits for Run to return.
func (r *running) stop() {
	r.cancel()
	<-r.done
}

// selfPresence is the room's presence telling the account its nickname.
func selfPresence(room, nick string) string {
	return `<presence from="` + room + `/` + nick + `"><x xmlns="` + nsMUCUser + `">` +
		`<status code="110"/><status code="210"/></x></presence>`
}
