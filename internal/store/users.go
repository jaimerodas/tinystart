package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User is a row of the users table. There is no separate start page record
// and there never was one. Columns is the width of this user's grid, and the
// groups hang off the user directly.
type User struct {
	ID              int64
	Email           string
	PasswordDigest  string
	Admin           bool
	Approved        bool
	ThemePreference string
	ColorPreference string
	SearchEngine    string
	Columns         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ValidThemes, ValidColors and ValidSearchEngines are what the preferences
// can be set to. The colors are User::VALID_COLORS, and the order is the
// order the palette is drawn in. Grey is not among them: it was dropped and
// migrated to teal. DuckDuckGo is first because it is the default a new
// account starts with.
var (
	ValidThemes        = []string{"system", "light", "dark"}
	ValidColors        = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}
	ValidSearchEngines = []string{"duckduckgo", "google", "kagi"}
)

// MaxColumns is the widest grid the editor offers. The narrowest is one, which
// is also what a new account starts at: an empty grid three columns wide reads
// as broken.
const MaxColumns = 6

// railsBcryptCost is Rails' BCrypt::Engine.cost, which has_secure_password
// used. Rails made the digests already in the database with it, and digests
// made here keep the same cost — one digest format, whoever wrote the row.
const railsBcryptCost = 12

// bcryptCost is a variable only so that the tests can turn it down. Hashing at
// cost 12 takes about a quarter of a second by design. A test suite that
// signs a hundred people up spends most of its time on it. That is the same
// reason Rails sets ActiveModel::SecurePassword.min_cost in the test
// environment. Nothing outside a test ever assigns to it.
var bcryptCost = railsBcryptCost

// UseCheapPasswordHashing drops the cost to the cheapest bcrypt allows and
// returns the function that puts it back. It exists for the tests in the
// packages above this one. A single hash at cost 12 takes a quarter of a
// second, and several seconds under -race. So a suite that signs a dozen
// people up spends all of its time here. Rails does the same thing with
// ActiveModel::SecurePassword.min_cost in the test environment.
//
// Nothing but a test can call it. A digest written at the cheap cost is a
// digest anyone can crack, and it stays in the database afterwards.
func UseCheapPasswordHashing() (restore func()) {
	previous := bcryptCost
	bcryptCost = bcrypt.MinCost
	return func() { bcryptCost = previous }
}

// maxPasswordLength is has_secure_password's limit, and bcrypt's: the
// algorithm ignores everything past the 72nd byte. So accepting a longer
// password means accepting a shorter one at sign-in too.
const maxPasswordLength = 72

// userColumns is the select list every user query shares, in a fixed order so
// that scanUser can be shared with it.
const userColumns = `id, email, password_digest, admin, approved,
	theme_preference, color_preference, search_engine, "columns", created_at, updated_at`

// CreateUser signs someone up. It strips and downcases the email first, so
// "  Me@Example.COM " and "me@example.com" are the same account, and
// CreateUser refuses the second one.
//
// CreateUser makes the very first user of an installation an approved admin.
// There is nobody else to approve them, and an install with no way in is not
// an install. Everyone after that arrives unapproved and waits.
func (db *DB) CreateUser(ctx context.Context, email, password string) (*User, error) {
	email = normalizeEmail(email)

	var user *User
	err := db.tx(ctx, func(tx *sql.Tx) error {
		// This validates fields in the same order app/models/user.rb declares
		// the validations. The page joins the messages with ", " and prints
		// them in that order.
		fields := passwordErrors(password)

		if email == "" {
			fields = append(fields, FieldError{"email", msgBlank})
		} else {
			taken, err := exists(ctx, tx, `SELECT 1 FROM users WHERE email = ?`, email)
			if err != nil {
				return err
			}
			if taken {
				fields = append(fields, FieldError{"email", msgTaken})
			}
		}

		if err := invalid(fields...); err != nil {
			return err
		}

		digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			return err
		}

		// "Is this the first user" and the insert have to be one transaction.
		// If they are not, two people signing up at the same moment both see
		// an empty table, and both become admins.
		any, err := exists(ctx, tx, `SELECT 1 FROM users`)
		if err != nil {
			return err
		}
		first := !any

		now := utcNow()
		result, err := tx.ExecContext(ctx,
			`INSERT INTO users (email, password_digest, admin, approved,
				theme_preference, color_preference, "columns", created_at, updated_at)
			 VALUES (?, ?, ?, ?, 'system', 'teal', 1, ?, ?)`,
			email, string(digest), railsBool(first), railsBool(first),
			railsTime(now), railsTime(now))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}

		user = &User{
			ID:              id,
			Email:           email,
			PasswordDigest:  string(digest),
			Admin:           first,
			Approved:        first,
			ThemePreference: "system",
			ColorPreference: "teal",
			SearchEngine:    "duckduckgo",
			Columns:         1,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// Authenticate returns the user when the password matches. It returns
// ErrNotFound when the password does not match, the same answer as for an
// email nobody has. This way, nobody can use a failed sign-in to find out
// who has an account here. Whether that user is allowed in is a separate
// question, and Approved answers it.
func (db *DB) Authenticate(ctx context.Context, email, password string) (*User, error) {
	user, err := db.UserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		// Hash the password against a digest that will never match, so that a
		// missing account takes as long to reject as a wrong password. Without
		// it the difference is a hundred milliseconds and an email address is
		// answerable by stopwatch. Rails does the same thing inside
		// authenticate_by.
		bcrypt.CompareHashAndPassword([]byte(dummyDigest), []byte(password))
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(password)) != nil {
		return nil, ErrNotFound
	}
	return user, nil
}

// dummyDigest is a real bcrypt digest at the real cost, of a password nobody
// will guess. Only its shape matters: it makes the comparison above do the
// same work as a genuine one.
const dummyDigest = "$2a$12$j/LU.VektUBzh2CC.SDgiuU20fIWhi7GNKHiTxOQpA5.VYYzFuoBG"

func (db *DB) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(db.sql.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// UserByEmail normalizes what it is given, so a sign-in form that was typed in
// capitals finds the account anyway.
func (db *DB) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(db.sql.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = ?`, normalizeEmail(email)))
}

// AnyUsers reports whether anyone has signed up. The sign-in page redirects to
// sign-up when nobody has: a form with no account behind it is a dead end.
func (db *DB) AnyUsers(ctx context.Context) (bool, error) {
	return exists(ctx, db.sql, `SELECT 1 FROM users`)
}

// AllUsers is the admin list, newest first. The id breaks a tie, which Rails
// left to the database. Two accounts created in the same microsecond are
// rare, but a test that shuffles is not worth the flake.
func (db *DB) AllUsers(ctx context.Context) ([]User, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, rows.Err()
}

// UpdatePreferences sets the theme, the color and the search engine, which
// are the things Settings can change about a user. The column count is not
// among them on purpose: it is the editor's, so that the page that shows the
// groups it names can answer a refusal.
func (db *DB) UpdatePreferences(ctx context.Context, userID int64, theme, color, searchEngine string) error {
	var fields []FieldError
	if !slices.Contains(ValidThemes, theme) {
		fields = append(fields, FieldError{"theme_preference", theme + " is not a valid theme"})
	}
	if !slices.Contains(ValidColors, color) {
		fields = append(fields, FieldError{"color_preference", color + " is not a valid color"})
	}
	if !slices.Contains(ValidSearchEngines, searchEngine) {
		fields = append(fields, FieldError{"search_engine", searchEngine + " is not a valid search engine"})
	}
	if err := invalid(fields...); err != nil {
		return err
	}

	return db.update(ctx, `UPDATE users SET theme_preference = ?, color_preference = ?,
		search_engine = ?, updated_at = ? WHERE id = ?`,
		theme, color, searchEngine, railsTime(utcNow()), userID)
}

// UpdateColumns widens or narrows the grid.
//
// It refuses to narrow past a column that still holds a group, because the
// grid only draws the columns it has. A group left beyond the limit vanishes
// from the start page and from the editor both, taking its own move and
// delete controls with it. Its tiles carry on showing up in the command bar.
// Refuse the change rather than hide someone's work.
func (db *DB) UpdateColumns(ctx context.Context, userID int64, columns int) error {
	if columns < 1 {
		return invalid(FieldError{"columns", "must be greater than 0"})
	}
	if columns > MaxColumns {
		return invalid(FieldError{"columns", fmt.Sprintf("must be less than or equal to %d", MaxColumns)})
	}

	return db.tx(ctx, func(tx *sql.Tx) error {
		var current int
		err := tx.QueryRowContext(ctx, `SELECT "columns" FROM users WHERE id = ?`, userID).Scan(&current)
		if err != nil {
			return notFound(err)
		}
		// Rails only runs this check when the count actually changed, and so
		// does this. A save that leaves the width alone must not start failing
		// because of a group that was already out of bounds.
		if current != columns {
			stranded, err := strandedGroups(ctx, tx, userID, columns)
			if err != nil {
				return err
			}
			if len(stranded) > 0 {
				return invalid(FieldError{"columns", strandedMessage(stranded)})
			}
		}

		_, err = tx.ExecContext(ctx, `UPDATE users SET "columns" = ?, updated_at = ? WHERE id = ?`,
			columns, railsTime(utcNow()), userID)
		return err
	})
}

// strandedGroup is a group that a narrower grid hides: just enough of it
// to write the refusal.
type strandedGroup struct {
	Name   string
	Column int
}

func strandedGroups(ctx context.Context, tx *sql.Tx, userID int64, columns int) ([]strandedGroup, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT name, "column" FROM start_page_groups WHERE user_id = ? AND "column" > ? ORDER BY "column"`,
		userID, columns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stranded []strandedGroup
	for rows.Next() {
		var group strandedGroup
		if err := rows.Scan(&group.Name, &group.Column); err != nil {
			return nil, err
		}
		stranded = append(stranded, group)
	}
	return stranded, rows.Err()
}

// strandedMessage is the sentence the editor puts in the notice, word for
// word as columns_leave_no_group_stranded wrote it. This includes the em
// dash and the quoted names in column order.
func strandedMessage(stranded []strandedGroup) string {
	names := make([]string, len(stranded))
	widest := 0
	for i, group := range stranded {
		names[i] = `"` + group.Name + `"`
		widest = max(widest, group.Column)
	}
	return fmt.Sprintf("can't be fewer than %d — that would hide %s. Move them first.",
		widest, toSentence(names))
}

// UpdatePassword changes a password after it makes sure that the current one
// is correct. The messages are the ones User#update_password produces, and
// so is the order. It makes sure that the existing password is correct
// before it looks at the new one.
func (db *DB) UpdatePassword(ctx context.Context, userID int64, existingPassword, newPassword string) error {
	user, err := db.UserByID(ctx, userID)
	if err != nil {
		return err
	}

	if existingPassword == "" {
		return invalid(FieldError{"existing_password", msgBlank})
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(existingPassword)) != nil {
		return invalid(FieldError{"existing_password", "is incorrect"})
	}
	// Longer than six, not at least six: the form has said "has to be longer"
	// since the first version and the rule behind it is > 6.
	if len([]rune(newPassword)) <= 6 {
		return invalid(FieldError{"new_password", "has to be longer"})
	}
	if fields := passwordErrors(newPassword); len(fields) > 0 {
		return invalid(fields...)
	}

	digest, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	return db.update(ctx, `UPDATE users SET password_digest = ?, updated_at = ? WHERE id = ?`,
		string(digest), railsTime(utcNow()), userID)
}

// ResetPassword sets a password without asking for the old one. It is what the
// reset-by-email flow lands on, where possession of the mailbox is the proof.
//
// Note that it does not apply UpdatePassword's "has to be longer" rule.
// PasswordsController#update was a plain save, so only has_secure_password's
// own validations ran there — the same asymmetry, kept rather than tidied.
// Tidying it refuses a password the deployed app accepts.
func (db *DB) ResetPassword(ctx context.Context, userID int64, newPassword string) error {
	if fields := passwordErrors(newPassword); len(fields) > 0 {
		return invalid(fields...)
	}

	digest, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	return db.update(ctx, `UPDATE users SET password_digest = ?, updated_at = ? WHERE id = ?`,
		string(digest), railsTime(utcNow()), userID)
}

// ToggleApproved flips the approval switch, which is the one thing the admin
// list does. It is a toggle rather than a setter because the button is one
// button. Reading the current value in the same transaction that writes it
// keeps two admins who click at once from cancelling each other out.
func (db *DB) ToggleApproved(ctx context.Context, userID int64) (*User, error) {
	err := db.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`UPDATE users SET approved = NOT approved, updated_at = ? WHERE id = ?`,
			railsTime(utcNow()), userID)
		if err != nil {
			return err
		}
		return mustHaveChanged(result)
	})
	if err != nil {
		return nil, err
	}
	return db.UserByID(ctx, userID)
}

// passwordErrors is has_secure_password's own two validations.
func passwordErrors(password string) []FieldError {
	switch {
	case password == "":
		return []FieldError{{"password", msgBlank}}
	// Rails counts characters here and bcrypt counts bytes, so a long enough
	// multibyte password passes the Rails validation. The Ruby bcrypt gem
	// then silently truncates it. Go's bcrypt refuses it instead, which is
	// the better answer, so both limits apply, and the message copies Rails'
	// own wording.
	case len([]rune(password)) > maxPasswordLength, len(password) > maxPasswordLength:
		return []FieldError{{"password", fmt.Sprintf(
			"is too long (maximum is %d characters)", maxPasswordLength)}}
	}
	return nil
}

// normalizeEmail is the `normalizes :email` declaration on the model. It
// strips and downcases an email before it is stored, compared, or looked up,
// so that capitalisation cannot produce a second account.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// toSentence is Array#to_sentence with Rails' defaults, including the serial
// comma before the last "and". It exists for exactly one message, but that
// message is asserted on character by character.
func toSentence(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

// scanUser reads one user from anything that can produce a row, which is what
// lets QueryRow and Rows share it.
func scanUser(row scanner) (*User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.PasswordDigest, &user.Admin, &user.Approved,
		&user.ThemePreference, &user.ColorPreference, &user.SearchEngine, &user.Columns,
		(*railsTime)(&user.CreatedAt), (*railsTime)(&user.UpdatedAt))
	if err != nil {
		return nil, notFound(err)
	}
	return &user, nil
}
