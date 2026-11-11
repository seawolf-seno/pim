package xmpp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDecodeResult checks reading an IQ answer: a payload, an error with or
// without a condition, an empty result, and garbage.
func TestDecodeResult(t *testing.T) {
	t.Parallel()

	decode := func(raw string, result any) error {
		return decodeResult(xml.NewDecoder(strings.NewReader(raw)), result)
	}

	var roster rosterQuery
	if err := decode(`<iq type="result">`+rosterAnswer+`</iq>`, &roster); err != nil || len(roster.Items) != 2 {
		t.Errorf("roster = %+v, %v", roster, err)
	}
	if err := decode(`<iq type="result"/>`, &roster); err != nil {
		t.Errorf("empty result = %v", err)
	}
	if err := decode(`<iq type="result"><query/></iq>`, nil); err != nil {
		t.Errorf("nil result = %v", err)
	}

	var refused errorIQ
	err := decode(`<iq type="error"><error type="auth"><forbidden xmlns="`+nsStanzas+`"/></error></iq>`, &roster)
	if !errors.As(err, &refused) || refused.condition != "forbidden" || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error answer = %v", err)
	}
	if err := decode(`<iq type="error"/>`, &roster); !errors.As(err, &refused) || refused.condition != "undefined-condition" {
		t.Errorf("error without a condition = %v", err)
	}
	if err := decode(`<iq type="result">`, &roster); err == nil {
		t.Error("decoded an unterminated answer")
	}
	if err := decode(`<iq type="result"><query xmlns="jabber:iq:roster"><item></query></iq>`, &roster); err == nil {
		t.Error("decoded a malformed payload")
	}
	if err := decode(`<iq type="error"><text/></iq>`, &roster); !errors.As(err, &refused) || refused.condition != "undefined-condition" {
		t.Errorf("error with a stray child = %v", err)
	}
	if err := decode(`<iq type="error"><error><forbidden></iq>`, &roster); err == nil {
		t.Error("decoded a malformed error")
	}
	if err := decode(``, &roster); err == nil {
		t.Error("decoded nothing")
	}
}

// tokenStream reads the tokens of raw one by one, the way a session hands
// an answer over: with no raw bytes behind it.
type tokenStream struct {
	d *xml.Decoder
}

// Token returns the next token.
func (s tokenStream) Token() (xml.Token, error) {
	tok, err := s.d.Token()
	if err != nil {
		return nil, err
	}

	return xml.CopyToken(tok), nil
}

// TestDecodeResult_OverTokens checks decoding an answer read as tokens, as
// from a real session, where ",innerxml" would be empty.
func TestDecodeResult_OverTokens(t *testing.T) {
	t.Parallel()

	raw := `<iq type="result" id="r1">` + rosterAnswer + `</iq>`
	d := xml.NewTokenDecoder(tokenStream{xml.NewDecoder(strings.NewReader(raw))})
	var roster rosterQuery
	if err := decodeResult(d, &roster); err != nil || len(roster.Items) != 2 || roster.Items[0].Name != "Mr. Okafor" {
		t.Errorf("roster over tokens = %+v, %v", roster, err)
	}

	raw = `<iq type="error"><error type="auth"><forbidden xmlns="` + nsStanzas + `"/></error></iq>`
	d = xml.NewTokenDecoder(tokenStream{xml.NewDecoder(strings.NewReader(raw))})
	var refused errorIQ
	if err := decodeResult(d, &roster); !errors.As(err, &refused) || refused.condition != "forbidden" {
		t.Errorf("error over tokens = %v", err)
	}
}

// TestStanzaDecoder checks decoding a stanza handed over the way mellium
// hands it to a handler: its start element apart, and the rest, including
// its end, as tokens.
func TestStanzaDecoder(t *testing.T) {
	t.Parallel()

	raw := `<presence from="biology@rooms.pim.test/Rowan Teague"><x xmlns="` + nsMUCUser + `"><status code="110"/></x></presence>`
	inner := xml.NewDecoder(strings.NewReader(raw))
	start, err := firstStart(inner)
	if err != nil {
		t.Fatal(err)
	}

	d, err := stanzaDecoder(tokenStream{inner}, start)
	if err != nil {
		t.Fatal(err)
	}
	var p inPresence
	if err := d.DecodeElement(&p, &start); err != nil || !p.isSelf() {
		t.Errorf("presence = %+v, %v", p, err)
	}
}

// timeoutError is a net.Error for a timed-out connection.
type timeoutError struct{}

// Error describes the timeout.
func (timeoutError) Error() string { return "i/o timeout" }

// Timeout reports that it is a timeout.
func (timeoutError) Timeout() bool { return true }

// Temporary reports that it may pass.
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

// TestSignInError checks which SASL failures mean a wrong password.
func TestSignInError(t *testing.T) {
	t.Parallel()

	if signInError(nil) != nil {
		t.Error("nil became an error")
	}
	for _, connection := range []error{io.EOF, io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded, timeoutError{}} {
		if errors.Is(signInError(connection), errSignIn) {
			t.Errorf("%v was taken as a wrong password", connection)
		}
	}
	if err := signInError(errors.New("not-authorized")); !errors.Is(err, errSignIn) {
		t.Errorf("a refusal = %v, want errSignIn", err)
	}
}

// TestSignIn_WrapsTheSASLFeature checks that the sign-in feature is SASL
// with its negotiation wrapped.
func TestSignIn_WrapsTheSASLFeature(t *testing.T) {
	t.Parallel()

	f := signIn("secret")
	if f.Name.Local != "mechanisms" || f.Negotiate == nil {
		t.Errorf("feature = %+v", f.Name)
	}
}

// TestTLSConfig checks trusting the system or only a certificate file.
func TestTLSConfig(t *testing.T) {
	t.Parallel()

	cfg, err := tlsConfig(settings{}, "pim.test")
	if err != nil || cfg.ServerName != "pim.test" || cfg.RootCAs != nil {
		t.Errorf("system trust = %+v, %v", cfg, err)
	}

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	writeCertificate(t, caFile)
	cfg, err = tlsConfig(settings{CAFile: caFile}, "pim.test")
	if err != nil || cfg.RootCAs == nil {
		t.Errorf("file trust = %+v, %v", cfg, err)
	}

	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsConfig(settings{CAFile: empty}, "pim.test"); !errors.Is(err, errNoCertificates) {
		t.Errorf("empty file = %v", err)
	}
	if _, err := tlsConfig(settings{CAFile: filepath.Join(dir, "missing.pem")}, "pim.test"); err == nil {
		t.Error("missing file succeeded")
	}
}

// TestDialMellium_Failures checks the ways dialling fails before a server
// answers: bad settings, an unreadable certificate file, nobody listening.
func TestDialMellium_Failures(t *testing.T) {
	t.Parallel()

	if _, err := dialMellium(t.Context(), settings{Address: "@@"}); err == nil {
		t.Error("dialled an invalid address")
	}
	if _, err := dialMellium(t.Context(), settings{Address: "a@pim.test", CAFile: "/nonexistent/ca.pem"}); err == nil {
		t.Error("dialled with a missing certificate file")
	}
	if _, err := dialMellium(t.Context(), settings{Address: "a@pim.test", Server: "bad host"}); err == nil {
		t.Error("dialled a bad server")
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := dialMellium(t.Context(), settings{Address: "a@pim.test", Server: addr}); err == nil {
		t.Error("dialled a closed port")
	}
}

// TestDialMellium_ServerHangsUp checks that a server closing the connection
// during negotiation fails the dial and closes the connection.
func TestDialMellium_ServerHangsUp(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close() // the point of the test: hang up at once
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := dialMellium(ctx, settings{Address: "a@pim.test", Server: listener.Addr().String()}); err == nil {
		t.Error("dial succeeded against a server that hung up")
	}
}

// writeCertificate writes a self-signed certificate as PEM to path.
func writeCertificate(t *testing.T, path string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "pim.test"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"pim.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
