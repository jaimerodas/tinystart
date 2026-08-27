package store

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUserNormalizesEmail(t *testing.T) {
	tests := []struct {
		name  string
		given string
		want  string
	}{
		{"strips surrounding space", "  test@example.com  ", "test@example.com"},
		{"downcases", "TEST@EXAMPLE.COM", "test@example.com"},
		{"both at once", "  TEST@Example.COM ", "test@example.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)

			user, err := db.CreateUser(t.Context(), test.given, "password123")
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			if user.Email != test.want {
				t.Errorf("email = %q, want %q", user.Email, test.want)
			}
		})
	}
}

func TestCreateUserValidations(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		want     []string
	}{
		{"no email", "", "password123", []string{"Email can't be blank"}},
		{"blank email", "   ", "password123", []string{"Email can't be blank"}},
		{"no password", "test@example.com", "", []string{"Password can't be blank"}},
		{"neither", "", "", []string{"Password can't be blank", "Email can't be blank"}},
		{"password past 72 bytes", "test@example.com", strings.Repeat("a", 73),
			[]string{"Password is too long (maximum is 72 characters)"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)

			_, err := db.CreateUser(t.Context(), test.email, test.password)
			assertInvalid(t, err, test.want...)
		})
	}
}

func TestCreateUserRefusesADuplicateEmail(t *testing.T) {
	db := newTestDB(t)
	newUser(t, db, "test@example.com")

	for _, email := range []string{"test@example.com", "TEST@EXAMPLE.COM", "  test@example.com "} {
		_, err := db.CreateUser(t.Context(), email, "password123")
		assertInvalid(t, err, "Email has already been taken")
	}
}

// There is nobody to approve the first account, and an install with no way in
// is not an install.
func TestCreateUserBootstrapsTheFirstUser(t *testing.T) {
	db := newTestDB(t)

	first, err := db.CreateUser(t.Context(), "first@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !first.Approved || !first.Admin {
		t.Errorf("first user approved=%v admin=%v, want both true", first.Approved, first.Admin)
	}

	second, err := db.CreateUser(t.Context(), "second@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if second.Approved || second.Admin {
		t.Errorf("second user approved=%v admin=%v, want both false", second.Approved, second.Admin)
	}
}

// One column, not three: a new grid is empty, and empty columns read as broken.
func TestCreateUserDefaults(t *testing.T) {
	db := newTestDB(t)

	user, err := db.CreateUser(t.Context(), "test@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	stored, err := db.UserByID(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if stored.ThemePreference != "system" {
		t.Errorf("theme = %q, want system", stored.ThemePreference)
	}
	if stored.ColorPreference != "teal" {
		t.Errorf("color = %q, want teal", stored.ColorPreference)
	}
	if stored.Columns != 1 {
		t.Errorf("columns = %d, want 1", stored.Columns)
	}
	if stored.SearchEngine != "duckduckgo" {
		t.Errorf("search engine = %q, want duckduckgo", stored.SearchEngine)
	}
	if stored.PasswordDigest == "password123" || stored.PasswordDigest == "" {
		t.Errorf("password_digest = %q, want a hash", stored.PasswordDigest)
	}
}

func TestAuthenticate(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	found, err := db.Authenticate(t.Context(), "test@example.com", "password123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if found.ID != user.ID {
		t.Errorf("id = %d, want %d", found.ID, user.ID)
	}

	// Capitalisation is not a different account.
	if _, err := db.Authenticate(t.Context(), "TEST@example.com", "password123"); err != nil {
		t.Errorf("Authenticate with a capitalised email: %v", err)
	}

	// A wrong password and an unknown address give the same answer. This way,
	// nobody can use a sign-in form to find out who has an account here.
	_, err = db.Authenticate(t.Context(), "test@example.com", "wrongpassword")
	assertNotFound(t, err)

	_, err = db.Authenticate(t.Context(), "nobody@example.com", "password123")
	assertNotFound(t, err)
}

// The Ruby side made the digest below, at the cost Rails uses:
//
//	bundle exec ruby -rbcrypt -e 'puts BCrypt::Password.create("password123", cost: 12)'
//
// Every password in production is one of these. If Go cannot make sure that
// they are correct, nobody can sign in after the cutover.
func TestAuthenticateAcceptsADigestRailsMade(t *testing.T) {
	const railsDigest = "$2a$12$EtqmeoUoVPpd432xuXr.1u1dG9BK5oqEVVXpHUxZfLDb5VQG1leBe"

	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	if _, err := db.sql.ExecContext(t.Context(),
		`UPDATE users SET password_digest = ? WHERE id = ?`, railsDigest, user.ID); err != nil {
		t.Fatalf("planting the Rails digest: %v", err)
	}

	if _, err := db.Authenticate(t.Context(), "test@example.com", "password123"); err != nil {
		t.Fatalf("Authenticate against a Rails digest: %v", err)
	}
	if _, err := db.Authenticate(t.Context(), "test@example.com", "wrongpassword"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the wrong password, got %v", err)
	}
}

// The other direction: a digest written here has to match the ones Rails
// left in the database, prefix and cost alike. One digest format, whoever
// wrote the row.
func TestCreateUserHashesAtRailsCost(t *testing.T) {
	bcryptCost = railsBcryptCost
	t.Cleanup(func() { bcryptCost = bcrypt.MinCost })

	db := newTestDB(t)
	user, err := db.CreateUser(t.Context(), "test@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if !strings.HasPrefix(user.PasswordDigest, "$2a$12$") {
		t.Errorf("digest = %q, want a $2a$12$ digest like BCrypt::Password.create makes",
			user.PasswordDigest)
	}
}

func TestUpdatePreferences(t *testing.T) {
	tests := []struct {
		name         string
		theme        string
		color        string
		searchEngine string
		want         []string
	}{
		{"a theme it does not know", "neon", "teal", "duckduckgo", []string{"Theme preference neon is not a valid theme"}},
		{"a colour it does not know", "dark", "chartreuse", "duckduckgo", []string{"Color preference chartreuse is not a valid color"}},
		// Grey left the palette and everyone holding it was migrated to teal.
		// Nothing can put it back.
		{"grey, which left the palette", "dark", "gray", "duckduckgo", []string{"Color preference gray is not a valid color"}},
		{"a search engine it does not know", "dark", "teal", "bing", []string{"Search engine bing is not a valid search engine"}},
		{"all three wrong", "neon", "gray", "bing", []string{
			"Theme preference neon is not a valid theme",
			"Color preference gray is not a valid color",
			"Search engine bing is not a valid search engine",
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			err := db.UpdatePreferences(t.Context(), user.ID, test.theme, test.color, test.searchEngine)
			assertInvalid(t, err, test.want...)
		})
	}

	t.Run("every listed theme, colour and search engine is accepted", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")

		for _, theme := range ValidThemes {
			for _, color := range ValidColors {
				for _, engine := range ValidSearchEngines {
					if err := db.UpdatePreferences(t.Context(), user.ID, theme, color, engine); err != nil {
						t.Fatalf("UpdatePreferences(%q, %q, %q): %v", theme, color, engine, err)
					}
				}
			}
		}

		stored, err := db.UserByID(t.Context(), user.ID)
		if err != nil {
			t.Fatalf("UserByID: %v", err)
		}
		if stored.ThemePreference != "dark" || stored.ColorPreference != "pink" || stored.SearchEngine != "kagi" {
			t.Errorf("stored %q/%q/%q, want dark/pink/kagi",
				stored.ThemePreference, stored.ColorPreference, stored.SearchEngine)
		}
	})
}

func TestUpdateColumnsBounds(t *testing.T) {
	tests := []struct {
		name    string
		columns int
		want    string
	}{
		{"zero", 0, "Columns must be greater than 0"},
		{"negative", -1, "Columns must be greater than 0"},
		{"past six", 7, "Columns must be less than or equal to 6"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			assertInvalid(t, db.UpdateColumns(t.Context(), user.ID, test.columns), test.want)
		})
	}
}

// Narrowing used to hide any group past the new limit. It vanished from the
// start page and from the editor both, so its move and delete controls were
// unreachable. Its tiles carried on showing up in the command bar.
func TestUpdateColumnsRefusesToStrandAGroup(t *testing.T) {
	t.Run("names the one group it would hide", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "Reading", 3)

		err := db.UpdateColumns(t.Context(), user.ID, 2)
		assertInvalid(t, err,
			`Columns can't be fewer than 3 — that would hide "Reading". Move them first.`)
	})

	t.Run("names every group it would hide, in column order", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "Reading", 3)
		newGroup(t, db, user.ID, "Work", 2)

		err := db.UpdateColumns(t.Context(), user.ID, 1)
		assertInvalid(t, err,
			`Columns can't be fewer than 3 — that would hide "Work" and "Reading". Move them first.`)
	})

	t.Run("allows a shrink that strands nothing", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "Reading", 1)

		if err := db.UpdateColumns(t.Context(), user.ID, 1); err != nil {
			t.Fatalf("UpdateColumns: %v", err)
		}
	})

	t.Run("allows widening whatever the groups look like", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "Reading", 3)

		if err := db.UpdateColumns(t.Context(), user.ID, 6); err != nil {
			t.Fatalf("UpdateColumns: %v", err)
		}
	})

	// The check costs a query, so it must not run when the width is unchanged.
	t.Run("leaves a rewrite of the same count alone", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "Reading", 3)

		if err := db.UpdateColumns(t.Context(), user.ID, 3); err != nil {
			t.Fatalf("UpdateColumns: %v", err)
		}
	})
}

func TestUpdatePassword(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		next     string
		want     string
	}{
		{"no current password", "", "newpassword", "Existing password can't be blank"},
		{"the wrong current password", "wrongpass", "newpassword", "Existing password is incorrect"},
		{"a new password of six", "password123", "abcdef", "New password has to be longer"},
		{"an empty new password", "password123", "", "New password has to be longer"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			err := db.UpdatePassword(t.Context(), user.ID, test.existing, test.next)
			assertInvalid(t, err, test.want)

			// Nothing changed on the way to being refused.
			if _, err := db.Authenticate(t.Context(), user.Email, "password123"); err != nil {
				t.Errorf("the old password stopped working: %v", err)
			}
		})
	}

	t.Run("changes it", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")

		if err := db.UpdatePassword(t.Context(), user.ID, "password123", "newpassword"); err != nil {
			t.Fatalf("UpdatePassword: %v", err)
		}

		if _, err := db.Authenticate(t.Context(), user.Email, "newpassword"); err != nil {
			t.Errorf("the new password does not work: %v", err)
		}
		if _, err := db.Authenticate(t.Context(), user.Email, "password123"); !errors.Is(err, ErrNotFound) {
			t.Errorf("the old password still works")
		}
	})
}

// The reset-by-email flow lands here: possession of the mailbox is the proof,
// so there is no current password to ask for.
func TestResetPassword(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	// No "has to be longer" here: PasswordsController#update was a plain save,
	// so only has_secure_password's own validations ever ran on this path.
	if err := db.ResetPassword(t.Context(), user.ID, "short"); err != nil {
		t.Fatalf("ResetPassword with a short password: %v", err)
	}
	assertInvalid(t, db.ResetPassword(t.Context(), user.ID, ""), "Password can't be blank")

	if err := db.ResetPassword(t.Context(), user.ID, "newpassword"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if _, err := db.Authenticate(t.Context(), user.Email, "newpassword"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
}

func TestToggleApproved(t *testing.T) {
	db := newTestDB(t)
	newUser(t, db, "first@example.com")
	second := newUser(t, db, "second@example.com")

	if second.Approved {
		t.Fatalf("the second user should not arrive approved")
	}

	approved, err := db.ToggleApproved(t.Context(), second.ID)
	if err != nil {
		t.Fatalf("ToggleApproved: %v", err)
	}
	if !approved.Approved {
		t.Errorf("approved = false after the first toggle")
	}

	again, err := db.ToggleApproved(t.Context(), second.ID)
	if err != nil {
		t.Fatalf("ToggleApproved: %v", err)
	}
	if again.Approved {
		t.Errorf("approved = true after the second toggle")
	}
}

func TestAnyUsersAndAllUsers(t *testing.T) {
	db := newTestDB(t)

	any, err := db.AnyUsers(t.Context())
	if err != nil {
		t.Fatalf("AnyUsers: %v", err)
	}
	if any {
		t.Errorf("AnyUsers = true on an empty database")
	}

	newUser(t, db, "first@example.com")
	newUser(t, db, "second@example.com")

	any, err = db.AnyUsers(t.Context())
	if err != nil {
		t.Fatalf("AnyUsers: %v", err)
	}
	if !any {
		t.Errorf("AnyUsers = false with two users")
	}

	users, err := db.AllUsers(t.Context())
	if err != nil {
		t.Fatalf("AllUsers: %v", err)
	}
	emails := make([]string, len(users))
	for i, user := range users {
		emails[i] = user.Email
	}
	assertEqualStrings(t, emails, []string{"second@example.com", "first@example.com"})
}

func TestUserLookupsMiss(t *testing.T) {
	db := newTestDB(t)

	_, err := db.UserByID(t.Context(), 404)
	assertNotFound(t, err)

	_, err = db.UserByEmail(t.Context(), "nobody@example.com")
	assertNotFound(t, err)
}

func TestToSentence(t *testing.T) {
	tests := []struct {
		given []string
		want  string
	}{
		{nil, ""},
		{[]string{"one"}, "one"},
		{[]string{"one", "two"}, "one and two"},
		{[]string{"one", "two", "three"}, "one, two, and three"},
	}

	for _, test := range tests {
		if got := toSentence(test.given); got != test.want {
			t.Errorf("toSentence(%q) = %q, want %q", test.given, got, test.want)
		}
	}
}
