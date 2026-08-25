package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreatePrincipalMakesSettings(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}

	// The scheduler must never meet a person without a main page, and nothing creates one
	// lazily — it is made with the account or not at all.
	page, err := s.PageByID(ctx, MainPageID(p.ID))
	if err != nil {
		t.Fatalf("PageByID(): %v", err)
	}
	if !page.IsMain || page.Slug != "" {
		t.Errorf("page = %+v, want the main page at the root", page)
	}
	if page.EditionSize != 60 || page.EditionInterval != 24*time.Hour {
		t.Errorf("page = %+v, want the daily defaults", page)
	}
}

func TestUsernamesAreTakenCaseInsensitively(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreatePrincipal(ctx, "Alice", "correct-horse", RoleUser); err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	_, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second CreatePrincipal() = %v, want ErrConflict", err)
	}
}

func TestAuthenticate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}

	// The name is matched the way the column is: case-insensitively.
	got, err := s.Authenticate(ctx, "ALICE", "correct-horse")
	if err != nil {
		t.Fatalf("Authenticate(): %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("Authenticate() returned %s, want %s", got.ID, p.ID)
	}

	// A wrong name and a wrong password must be indistinguishable, or the login form
	// becomes a list of who has an account here.
	_, wrongPassword := s.Authenticate(ctx, "alice", "not-it")
	_, wrongName := s.Authenticate(ctx, "nobody", "not-it")
	if wrongPassword == nil || wrongName == nil {
		t.Fatal("Authenticate() accepted bad credentials")
	}
	if wrongPassword.Error() != wrongName.Error() {
		t.Errorf("a wrong password says %q but an unknown name says %q", wrongPassword, wrongName)
	}
}

func TestAuthenticateRefusesADisabledAccount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if err := s.SetDisabled(ctx, p.ID, true); err != nil {
		t.Fatalf("SetDisabled(): %v", err)
	}
	if _, err := s.Authenticate(ctx, "alice", "correct-horse"); err == nil {
		t.Fatal("a disabled account logged in")
	}
}

// Changing a password and ending the sessions it opened cannot come apart, or "this signs
// me out everywhere" is not true.
func TestSetPasswordEndsSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if err := s.CreateSession(ctx, "token", p.ID, s.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	if err := s.SetPassword(ctx, p.ID, "a-different-one"); err != nil {
		t.Fatalf("SetPassword(): %v", err)
	}
	if _, err := s.SessionByToken(ctx, "token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionByToken() after a password change = %v, want ErrNotFound", err)
	}
}

func TestDisablingEndsSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if err := s.CreateSession(ctx, "token", p.ID, s.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	if err := s.SetDisabled(ctx, p.ID, true); err != nil {
		t.Fatalf("SetDisabled(): %v", err)
	}
	if _, err := s.SessionByToken(ctx, "token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionByToken() after disabling = %v, want ErrNotFound", err)
	}
}

func TestValidateUsername(t *testing.T) {
	for _, ok := range []string{"al", "alice", "Alice.B", "a_b-c", "user2"} {
		if err := ValidateUsername(ok); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "a", "alice smith", "alice!", "_alice", strings.Repeat("a", 33)} {
		if err := ValidateUsername(bad); err == nil {
			t.Errorf("ValidateUsername(%q) = nil, want an error", bad)
		}
	}
}

// Bcrypt hashes only the first 72 bytes, so anything longer would be silently truncated —
// two different passwords that both work. Refusing is honest.
func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Error("ValidatePassword accepted a five-character password")
	}
	if err := ValidatePassword(strings.Repeat("a", 73)); err == nil {
		t.Error("ValidatePassword accepted a password bcrypt would truncate")
	}
	if err := ValidatePassword(strings.Repeat("a", 72)); err != nil {
		t.Errorf("ValidatePassword(72 bytes) = %v, want nil", err)
	}
}

func TestSessionExpiryIsAppliedInTheQuery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	start := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	at(t, s, start)

	p, err := s.CreatePrincipal(ctx, "alice", "correct-horse", RoleUser)
	if err != nil {
		t.Fatalf("CreatePrincipal(): %v", err)
	}
	if err := s.CreateSession(ctx, "token", p.ID, start.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	at(t, s, start.Add(30*time.Minute))
	if _, err := s.SessionByToken(ctx, "token"); err != nil {
		t.Fatalf("SessionByToken() inside the window: %v", err)
	}

	at(t, s, start.Add(2*time.Hour))
	if _, err := s.SessionByToken(ctx, "token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionByToken() past expiry = %v, want ErrNotFound", err)
	}
}

// The name somebody's published pages will live under.
func TestAPublicNameIsChosenAndCanBeTakenBack(t *testing.T) {
	s := testStore(t)
	alice, _ := s.CreatePrincipal(t.Context(), "alice", "correct-horse", RoleUser)

	// Nobody has one until they ask.
	if alice.Slug != "" {
		t.Errorf("a new account already has the public name %q", alice.Slug)
	}

	if _, err := s.SetPublicName(t.Context(), alice.ID, "Misha"); err != nil {
		t.Fatalf("SetPublicName(): %v", err)
	}
	got, _ := s.PrincipalByID(t.Context(), alice.ID)
	// Lower-cased on the way in, because it ends up in a URL and two spellings of one name
	// would be two addresses for one person.
	if got.Slug != "misha" {
		t.Errorf("public name = %q, want it folded to lower case", got.Slug)
	}

	// Changed at will. Nothing stores the address, so every published page moves with it —
	// which is the cost as well as the feature.
	if _, err := s.SetPublicName(t.Context(), alice.ID, "mv"); err != nil {
		t.Fatalf("SetPublicName(): %v", err)
	}
	if got, _ := s.PrincipalByID(t.Context(), alice.ID); got.Slug != "mv" {
		t.Errorf("public name = %q, want it changed", got.Slug)
	}

	// And taken back.
	if _, err := s.SetPublicName(t.Context(), alice.ID, ""); err != nil {
		t.Fatalf("SetPublicName(): %v", err)
	}
	if got, _ := s.PrincipalByID(t.Context(), alice.ID); got.Slug != "" {
		t.Errorf("public name = %q, want it gone", got.Slug)
	}
}

// Two people cannot answer to one name, and the refusal says nothing about who has it.
func TestAPublicNameIsTakenOnlyOnce(t *testing.T) {
	s := testStore(t)
	alice, _ := s.CreatePrincipal(t.Context(), "alice", "correct-horse", RoleUser)
	bob, _ := s.CreatePrincipal(t.Context(), "bob", "correct-horse", RoleUser)

	if _, err := s.SetPublicName(t.Context(), alice.ID, "misha"); err != nil {
		t.Fatalf("SetPublicName(): %v", err)
	}
	_, err := s.SetPublicName(t.Context(), bob.ID, "misha")
	if !errors.Is(err, ErrConflict) {
		t.Errorf("second claim on a name = %v, want a conflict", err)
	}
	// The wording is about the name, not about Alice: whether she exists is not Bob's
	// business, and the answer he needs is the same either way.
	if err != nil && strings.Contains(err.Error(), "alice") {
		t.Errorf("the refusal names the account that holds it: %v", err)
	}

	// Emptiness is not a name, so any number of accounts can be without one.
	if _, err := s.SetPublicName(t.Context(), alice.ID, ""); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if _, err := s.SetPublicName(t.Context(), bob.ID, ""); err != nil {
		t.Errorf("a second account could not be nameless: %v", err)
	}
}

func TestAPublicNameHasToLookLikeOne(t *testing.T) {
	s := testStore(t)
	alice, _ := s.CreatePrincipal(t.Context(), "alice", "correct-horse", RoleUser)

	for _, bad := range []string{"Misha Vyrtsev", "misha/comics", "misha_v", "-misha", "misha-", "ми"} {
		if _, err := s.SetPublicName(t.Context(), alice.ID, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("SetPublicName(%q) = %v, want it refused", bad, err)
		}
	}
	if _, err := s.SetPublicName(t.Context(), alice.ID, strings.Repeat("a", MaxSlug+1)); !errors.Is(err, ErrInvalid) {
		t.Errorf("an over-long name was accepted")
	}
}
