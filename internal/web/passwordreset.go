package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
)

// passwordResetLifetime is how long a reset link works for. It is Rails'
// `generates_token_for :password_reset, expires_in: 15.minutes`.
const passwordResetLifetime = 15 * time.Minute

// passwordResetPurpose is mixed into the signature so that a reset token
// cannot be presented as a session cookie or the other way round. It is the
// same job Rails' `purpose` served.
const passwordResetPurpose = "password_reset"

// errBadResetToken is every way a token can fail to name a user: forged,
// expired, malformed, or made before a password change. They are one error
// because the page says one thing — "Password reset link is invalid or has
// expired" — and because telling them apart helps someone forging one.
var errBadResetToken = errors.New("web: password reset token is not valid")

// passwordResetToken makes the token that goes in the mail.
//
// It carries three things, signed: who it is for, when it stops working, and a
// fingerprint of the password digest as it is now. The fingerprint is what
// makes a token single-use without a table to write it down in. Resetting the
// password changes the digest, which changes the fingerprint, which makes
// every token issued before then fail verification. Rails got the same
// property by keying generates_token_for on password_salt.
func (s *Server) passwordResetToken(user *store.User) string {
	payload := strings.Join([]string{
		strconv.FormatInt(user.ID, 10),
		strconv.FormatInt(s.now().Add(passwordResetLifetime).Unix(), 10),
		digestFingerprint(user.PasswordDigest),
	}, ":")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return s.signValue(passwordResetPurpose, encoded)
}

// userFromPasswordResetToken is the reverse: signature, then expiry, then the
// fingerprint, then the user. The order matters. Nothing touches the database
// before this function makes sure the signature is valid. That way an
// attacker cannot make the app do work by sending rubbish.
func (s *Server) userFromPasswordResetToken(ctx context.Context, token string) (*store.User, error) {
	encoded, err := s.verifyValue(passwordResetPurpose, token)
	if err != nil {
		return nil, errBadResetToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errBadResetToken
	}

	parts := strings.Split(string(payload), ":")
	if len(parts) != 3 {
		return nil, errBadResetToken
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, errBadResetToken
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, errBadResetToken
	}
	if !s.now().Before(time.Unix(expiresAt, 0)) {
		return nil, errBadResetToken
	}

	user, err := s.db.UserByID(ctx, id)
	if err != nil {
		return nil, errBadResetToken
	}
	if digestFingerprint(user.PasswordDigest) != parts[2] {
		return nil, errBadResetToken
	}
	return user, nil
}

// digestFingerprint is a short hash of the password digest. Short because it
// only has to change when the digest does. Putting the digest itself — or
// enough of it to attack offline — inside a token is risky, because that
// token travels by mail. Doing so hands out the thing the whole scheme
// protects.
func digestFingerprint(digest string) string {
	sum := sha256.Sum256([]byte(digest))
	return hex.EncodeToString(sum[:8])
}
