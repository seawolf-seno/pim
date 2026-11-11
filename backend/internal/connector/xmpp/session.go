package xmpp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"mellium.im/sasl"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/jid"
)

// errSignIn reports that the server refused the account's address or
// password. The connector stops retrying on it and asks for new settings,
// rather than sending a wrong password to the server every few seconds.
var errSignIn = errors.New("xmpp: the server refused the address or password")

// errNoCertificates reports a CAFile with no certificate in it.
var errNoCertificates = errors.New("xmpp: no certificates in the certificate file")

// link is one signed-in XMPP connection, the seam between the connector and
// the XMPP library: the connector's logic runs against a fake link in
// tests, and against melliumLink in the helper. It has four methods
// because a connection is used in exactly these four ways - one-way
// stanzas, requests that wait for an answer, the stream of stanzas the
// server sends, and closing - and splitting them would only spread one
// connection across several values.
type link interface {
	// send writes one stanza (a message, presence or IQ reply).
	send(ctx context.Context, v any) error

	// request sends an IQ get and decodes its result's payload into
	// result, returning an error for an error reply.
	request(ctx context.Context, iq outIQ, result any) error

	// serve reads the stanzas the server sends, handing each to handle,
	// until the connection closes. It returns the reason it stopped.
	serve(handle func(d *xml.Decoder, start xml.StartElement) error) error

	// close ends the session and its connection; serve then returns.
	close() error
}

// dialFunc opens a signed-in link with s.
type dialFunc func(ctx context.Context, s settings) (link, error)

// melliumLink is a link over a mellium.im/xmpp session.
type melliumLink struct {
	session *xmpp.Session
}

// dialMellium connects to the account's server, requires TLS, signs in
// with SASL and binds a resource.
func dialMellium(ctx context.Context, s settings) (link, error) {
	origin, err := jid.Parse(s.Address)
	if err != nil {
		return nil, fmt.Errorf("xmpp: dial: %w", err)
	}
	cfg, err := tlsConfig(s, origin.Domainpart())
	if err != nil {
		return nil, err
	}
	addr, err := serverAddress(s)
	if err != nil {
		return nil, err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("xmpp: dial: %w", err)
	}

	session, err := xmpp.NewClientSession(ctx, origin, conn,
		xmpp.StartTLS(cfg), signIn(s.Password), xmpp.BindResource())
	if err != nil {
		closeErr := conn.Close()

		return nil, fmt.Errorf("xmpp: dial: %w", errors.Join(err, closeErr))
	}

	return &melliumLink{session: session}, nil
}

// tlsConfig trusts the system's certificates, or only those in s.CAFile
// when it is set, for a server named domain.
func tlsConfig(s settings, domain string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12}
	if s.CAFile == "" {
		return cfg, nil
	}

	pem, err := os.ReadFile(s.CAFile)
	if err != nil {
		return nil, fmt.Errorf("xmpp: certificate file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errNoCertificates
	}
	cfg.RootCAs = pool

	return cfg, nil
}

// signIn is SASL with the strongest mechanisms first, whose failures that
// are not about the connection itself become errSignIn. mellium reports a
// refused password with an internal error type, so the step it happens in
// is what tells it apart.
func signIn(password string) xmpp.StreamFeature {
	feature := xmpp.SASL("", password,
		sasl.ScramSha256Plus, sasl.ScramSha256, sasl.ScramSha1Plus, sasl.ScramSha1, sasl.Plain)
	negotiate := feature.Negotiate
	feature.Negotiate = func(ctx context.Context, session *xmpp.Session, data any) (xmpp.SessionState, io.ReadWriter, error) {
		mask, rw, err := negotiate(ctx, session, data)

		return mask, rw, signInError(err)
	}

	return feature
}

// signInError turns an error from the SASL step into errSignIn, unless it
// is about the connection (closed, timed out, cancelled).
func signInError(err error) error {
	var netErr net.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &netErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return fmt.Errorf("%w: %w", errSignIn, err)
	}
}

// send writes v as one stanza.
func (l *melliumLink) send(ctx context.Context, v any) error {
	if err := l.session.Encode(ctx, v); err != nil {
		return fmt.Errorf("xmpp: send: %w", err)
	}

	return nil
}

// request sends iq and waits for its answer.
func (l *melliumLink) request(ctx context.Context, iq outIQ, result any) error {
	r, err := l.session.EncodeIQ(ctx, iq)
	if err != nil {
		return fmt.Errorf("xmpp: request: %w", err)
	}
	defer closeQuietly(r)

	return decodeResult(xml.NewTokenDecoder(r), result)
}

// closeQuietly closes an IQ response reader. Closing only tells the
// session it may resume reading; there is nothing to do if it fails.
func closeQuietly(r io.Closer) {
	_ = r.Close() // see the doc comment: a failed close has no consequence here
}

// serve hands every stanza the server sends to handle.
func (l *melliumLink) serve(handle func(d *xml.Decoder, start xml.StartElement) error) error {
	err := l.session.Serve(xmpp.HandlerFunc(func(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
		d, err := stanzaDecoder(t, *start)
		if err != nil {
			return nil //nolint:nilerr // a stanza that can't be read is skipped, not fatal to the session
		}

		return handle(d, *start)
	}))
	if err != nil {
		return fmt.Errorf("xmpp: serve: %w", err)
	}

	return nil
}

// stanzaDecoder is a decoder positioned just inside stanza start, whose
// remaining tokens (up to and including its end) come from r. mellium hands
// a handler the start element separately from r, but encoding/xml checks
// that every end matches a start it read itself, so the start is replayed
// through the decoder first.
func stanzaDecoder(r xml.TokenReader, start xml.StartElement) (*xml.Decoder, error) {
	d := xml.NewTokenDecoder(xmlstream.MultiReader(xmlstream.Token(start), r))
	if _, err := d.Token(); err != nil {
		return nil, fmt.Errorf("xmpp: read stanza: %w", err)
	}

	return d, nil
}

// close ends the session, then the connection under it.
func (l *melliumLink) close() error {
	return errors.Join(l.session.Close(), l.session.Conn().Close())
}

// errorIQ reports an IQ request the server answered with an error.
type errorIQ struct {
	condition string
}

// Error names the condition only: an error's text comes from the server and
// is not needed to decide anything.
func (e errorIQ) Error() string {
	return "xmpp: request refused: " + e.condition
}

// decodeResult reads one IQ answer from d: an error reply becomes an
// errorIQ, and a result's first child element is decoded into result. It
// walks the tokens itself rather than using ",innerxml", which is empty
// when d reads tokens from a session instead of bytes.
func decodeResult(d *xml.Decoder, result any) error {
	iq, err := firstStart(d)
	if err != nil {
		return fmt.Errorf("xmpp: request: %w", err)
	}
	isError := attr(iq, "type") == typeError

	for {
		tok, err := d.Token()
		if err != nil {
			return fmt.Errorf("xmpp: request: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return decodeChild(d, t, isError, result)
		case xml.EndElement:
			if isError {
				return errorIQ{condition: "undefined-condition"}
			}

			return nil
		}
	}
}

// decodeChild decodes the first child of an IQ answer: the error of an
// error reply, or the payload of a result into result.
func decodeChild(d *xml.Decoder, child xml.StartElement, isError bool, result any) error {
	switch {
	case isError && child.Name.Local == "error":
		var e stanzaError
		if err := d.DecodeElement(&e, &child); err != nil {
			return fmt.Errorf("xmpp: request: %w", err)
		}

		return errorIQ{condition: e.condition()}
	case isError:
		return errorIQ{condition: "undefined-condition"}
	case result == nil:
		return nil
	}
	if err := d.DecodeElement(result, &child); err != nil {
		return fmt.Errorf("xmpp: request: %w", err)
	}

	return nil
}

// firstStart reads tokens up to the first start element.
func firstStart(d *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start, nil
		}
	}
}

// attr is the value of start's attribute named local, or "".
func attr(start xml.StartElement, local string) string {
	for _, a := range start.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}

	return ""
}
