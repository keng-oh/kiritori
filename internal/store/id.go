package store

import (
	"crypto/rand"
	"math/big"
)

const (
	idAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	idLength   = 10
)

// newID returns a random base62 string. Collisions are handled by the caller
// retrying on a unique violation.
func newID() (string, error) {
	max := big.NewInt(int64(len(idAlphabet)))
	b := make([]byte, idLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = idAlphabet[n.Int64()]
	}
	return string(b), nil
}
