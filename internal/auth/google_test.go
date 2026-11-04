package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const (
	testIssuer   = "https://issuer.test"
	testClientID = "client-id"
	testNonce    = "nonce-1"
	testSub      = "sub-123"
)

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	s, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func claims(mod func(map[string]any)) map[string]any {
	c := map[string]any{
		"iss":   testIssuer,
		"aud":   testClientID,
		"sub":   testSub,
		"nonce": testNonce,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	if mod != nil {
		mod(c)
	}
	return c
}

// newTestGoogle returns a Google whose token endpoint replies with idToken,
// and whose verifier trusts only verifyKey. The last token request is kept in got.
func newTestGoogle(t *testing.T, idToken string, verifyKey *rsa.PrivateKey) (*Google, *url.Values) {
	t.Helper()
	got := &url.Values{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		*got = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "id_token": idToken,
		})
	}))
	t.Cleanup(ts.Close)
	keys := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&verifyKey.PublicKey}}
	g := newGoogle(testClientID, "secret", "http://localhost/auth/callback", testIssuer,
		oauth2.Endpoint{AuthURL: ts.URL + "/auth", TokenURL: ts.URL + "/token"}, keys)
	return g, got
}

func TestExchange(t *testing.T) {
	key := newKey(t)
	other := newKey(t)

	tests := []struct {
		name    string
		signer  *rsa.PrivateKey
		mod     func(map[string]any)
		nonce   string
		wantErr bool
	}{
		{"ok", key, nil, testNonce, false},
		{"nonce mismatch", key, nil, "other-nonce", true},
		{"empty expected nonce", key, func(c map[string]any) { delete(c, "nonce") }, "", true},
		{"aud mismatch", key, func(c map[string]any) { c["aud"] = "someone-else" }, testNonce, true},
		{"iss mismatch", key, func(c map[string]any) { c["iss"] = "https://evil.test" }, testNonce, true},
		{"expired", key, func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, testNonce, true},
		{"signed by another key", other, nil, testNonce, true},
		{"empty sub", key, func(c map[string]any) { c["sub"] = "" }, testNonce, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := newTestGoogle(t, sign(t, tt.signer, claims(tt.mod)), key)
			sub, err := g.Exchange(context.Background(), "code", "verifier", tt.nonce)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got sub %q", sub)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if sub != testSub {
				t.Fatalf("sub = %q, want %q", sub, testSub)
			}
		})
	}
}

func TestExchangeSendsPKCEVerifier(t *testing.T) {
	key := newKey(t)
	g, got := newTestGoogle(t, sign(t, key, claims(nil)), key)
	if _, err := g.Exchange(context.Background(), "the-code", "the-verifier", testNonce); err != nil {
		t.Fatal(err)
	}
	if got.Get("code") != "the-code" || got.Get("code_verifier") != "the-verifier" {
		t.Fatalf("token request = %v", *got)
	}
}

func TestExchangeNoIDToken(t *testing.T) {
	key := newKey(t)
	g, _ := newTestGoogle(t, "", key)
	if _, err := g.Exchange(context.Background(), "code", "v", testNonce); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthCodeURL(t *testing.T) {
	key := newKey(t)
	g, _ := newTestGoogle(t, "", key)
	u, err := url.Parse(g.AuthCodeURL("st", "no", "ver"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "st" || q.Get("nonce") != "no" || q.Get("client_id") != testClientID ||
		q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier("ver") {
		t.Fatalf("unexpected query: %v", q)
	}
	if q.Get("scope") != "openid" {
		t.Fatalf("scope = %q, want only openid", q.Get("scope"))
	}
	if !strings.HasPrefix(u.String(), "http://127.0.0.1:") {
		t.Fatalf("unexpected url %s", u)
	}
}
