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

// invitationLifetime is how long an invitation link works for. It is longer
// than a reset because nobody is waiting at the screen for it: the mail can
// sit in an inbox for days before it is read. The invitation mail and the
// Users page say "7 days".
const invitationLifetime = 7 * 24 * time.Hour

// The purposes are mixed into the signature, so a token made for one job
// cannot do another: a reset token cannot accept an invitation or be a
// session cookie, and the other way round. It is the same job Rails'
// `purpose` served.
const (
	passwordResetPurpose = "password_reset"
	invitationPurpose    = "invitation"
)

// errBadToken is every way a token can fail to name a user: forged,
// expired, malformed, or made before a password change. They are one error
// because the page says one thing — the link is invalid or has expired — and
// because telling them apart helps someone forging one.
var errBadToken = errors.New("web: token is not valid")

func (s *Server) passwordResetToken(user *store.User) string {
	return s.userToken(passwordResetPurpose, passwordResetLifetime, user)
}

func (s *Server) userFromPasswordResetToken(ctx context.Context, token string) (*store.User, error) {
	return s.userFromToken(ctx, passwordResetPurpose, token)
}

// invitationToken is a token like a reset token. The fingerprint is of the
// empty digest an invited user has, so setting the password ends it too.
func (s *Server) invitationToken(user *store.User) string {
	return s.userToken(invitationPurpose, invitationLifetime, user)
}

func (s *Server) userFromInvitationToken(ctx context.Context, token string) (*store.User, error) {
	return s.userFromToken(ctx, invitationPurpose, token)
}

// userToken makes the token that goes in the mail.
//
// It carries three things, signed: who it is for, when it stops working, and a
// fingerprint of the password digest as it is now. The fingerprint is what
// makes a token single-use without a table to write it down in. Resetting the
// password changes the digest, which changes the fingerprint, which makes
// every token issued before then fail verification. Rails got the same
// property by keying generates_token_for on password_salt.
func (s *Server) userToken(purpose string, lifetime time.Duration, user *store.User) string {
	payload := strings.Join([]string{
		strconv.FormatInt(user.ID, 10),
		strconv.FormatInt(s.now().Add(lifetime).Unix(), 10),
		digestFingerprint(user.PasswordDigest),
	}, ":")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return s.signValue(purpose, encoded)
}

// userFromToken is the reverse: signature, then expiry, then the
// fingerprint, then the user. The order matters. Nothing touches the database
// before this function makes sure the signature is valid. That way an
// attacker cannot make the app do work by sending rubbish.
func (s *Server) userFromToken(ctx context.Context, purpose, token string) (*store.User, error) {
	encoded, err := s.verifyValue(purpose, token)
	if err != nil {
		return nil, errBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errBadToken
	}

	parts := strings.Split(string(payload), ":")
	if len(parts) != 3 {
		return nil, errBadToken
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, errBadToken
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, errBadToken
	}
	if !s.now().Before(time.Unix(expiresAt, 0)) {
		return nil, errBadToken
	}

	user, err := s.db.UserByID(ctx, id)
	if err != nil {
		return nil, errBadToken
	}
	if digestFingerprint(user.PasswordDigest) != parts[2] {
		return nil, errBadToken
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
