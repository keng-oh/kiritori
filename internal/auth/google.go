// Package auth talks to Google's OpenID Connect endpoints.
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

const (
	googleIssuer   = "https://accounts.google.com"
	googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"
)

// ErrInvalidToken is returned when the code exchange or ID token validation fails.
var ErrInvalidToken = errors.New("auth: invalid id token")

type Google struct {
	cfg      oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewGoogle does not touch the network until a login is attempted:
// endpoints are fixed and signing keys are fetched lazily.
func NewGoogle(ctx context.Context, clientID, clientSecret, redirectURL string) *Google {
	return newGoogle(clientID, clientSecret, redirectURL, googleIssuer,
		endpoints.Google, oidc.NewRemoteKeySet(ctx, googleCertsURL))
}

func newGoogle(clientID, clientSecret, redirectURL, issuer string, ep oauth2.Endpoint, keys oidc.KeySet) *Google {
	return &Google{
		cfg: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     ep,
			// 識別に使うのは sub だけ。氏名・メールアドレスは要求しない
			Scopes: []string{oidc.ScopeOpenID},
		},
		verifier: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: clientID}),
	}
}

func (g *Google) AuthCodeURL(state, nonce, verifier string) string {
	return g.cfg.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the authorization code for an ID token, verifies it, and
// returns the user's stable subject identifier. go-oidc does not check the
// nonce, so it is compared here.
func (g *Google) Exchange(ctx context.Context, code, verifier, nonce string) (string, error) {
	tok, err := g.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("auth: exchange: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return "", fmt.Errorf("%w: no id_token in response", ErrInvalidToken)
	}
	idToken, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return "", fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}
	if idToken.Subject == "" {
		return "", fmt.Errorf("%w: empty subject", ErrInvalidToken)
	}
	return idToken.Subject, nil
}
