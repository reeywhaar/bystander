package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"bystander/internal/store"
)

// schedule asks for the signed-in account to be erased.
func (h *harness) schedule(password string) *http.Response {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/account/deletion", map[string]string{"password": password})
}

func TestDeletionIsScheduledRatherThanDone(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	// A second administrator, so the first is not the last one.
	h.secondAdmin("bob")

	var body deletionBody
	h.expect(h.schedule(harnessPassword), http.StatusOK, &body)

	if body.PurgeAt-body.DeletedAt != int64(store.DeletionGrace.Seconds()) {
		t.Errorf("the grace period is %ds, want %ds",
			body.PurgeAt-body.DeletedAt, int64(store.DeletionGrace.Seconds()))
	}

	// Nothing is gone yet, and the account can still be signed into — which is the whole
	// point of the week.
	if _, err := h.store.PrincipalByUsername(t.Context(), "ada"); err != nil {
		t.Fatalf("the account was erased immediately: %v", err)
	}

	// Every session ended with the request, including the one that made it.
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusUnauthorized, nil)
}

// Being signed in is not the same as knowing the password, and the difference is what stops
// a borrowed session becoming a destroyed account.
func TestDeletionNeedsThePassword(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	h.secondAdmin("bob")

	h.expect(h.schedule("not-the-password"), http.StatusBadRequest, nil)
	h.expect(h.schedule(""), http.StatusBadRequest, nil)

	// Still signed in, and still not scheduled.
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)
	p, err := h.store.PrincipalByUsername(t.Context(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	if p.ScheduledForDeletion() {
		t.Error("a refused password still scheduled the account for erasure")
	}
}

// The stronger of the two protections: even a successful deletion is undone by the owner
// doing the ordinary thing.
func TestSigningInCallsTheDeletionOff(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	h.secondAdmin("bob")

	h.expect(h.schedule(harnessPassword), http.StatusOK, nil)

	h.expect(h.do(http.MethodPost, "/api/login",
		map[string]string{"username": "ada", "password": harnessPassword}), http.StatusNoContent, nil)

	p, err := h.store.PrincipalByUsername(t.Context(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	if p.ScheduledForDeletion() {
		t.Fatal("signing in did not withdraw the request")
	}

	// And it is said rather than done silently: somebody who asked, forgot, and signed in
	// a fortnight later is owed the news.
	var account accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &account)
	if account.DeletionCancelledAt == 0 {
		t.Error("the account does not say the request was withdrawn")
	}
}

// Pressing it twice does not quietly buy another week.
func TestAskingTwiceKeepsTheFirstDate(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	h.secondAdmin("bob")

	var first deletionBody
	h.expect(h.schedule(harnessPassword), http.StatusOK, &first)

	// Signing in withdrew it, so ask again from a fresh session and confirm the second ask
	// is a fresh date rather than an extension of a live one. The extension case is the
	// store's: ScheduleDeletion over an account already scheduled answers with the date it
	// already has.
	deleted, err := h.store.PrincipalByUsername(t.Context(), "ada")
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.store.ScheduleDeletion(t.Context(), deleted.ID, harnessPassword)
	if err != nil {
		t.Fatal(err)
	}
	if again.Unix() != first.DeletedAt {
		t.Errorf("asking twice moved the date from %d to %d", first.DeletedAt, again.Unix())
	}
}

// There is no recovery from an instance with no administrator that does not involve a shell
// on the host, and deleting yourself is a likelier way to get there than deleting a colleague.
func TestTheLastAdministratorCannotLeave(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	res := h.schedule(harnessPassword)
	h.expect(res, http.StatusConflict, nil)

	// Still there, and still signed in.
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)
}

// And the page says so up front rather than letting somebody find out by typing their
// password into a danger button.
func TestTheAccountSaysWhenItIsTheLastAdministrator(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")

	var account accountBody
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &account)
	if !account.LastAdmin {
		t.Fatal("the only administrator is not told they are the only one")
	}

	// A second one, and the answer changes.
	h.secondAdmin("bob")
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, &account)
	if account.LastAdmin {
		t.Error("still reported as the last administrator with two of them")
	}
}

// An ordinary account is not the last administrator, whatever else is true of the instance.
func TestAnOrdinaryAccountCanLeave(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	bob := h.signInAsSomebodyElse("bob")

	var account accountBody
	h.expect(h.doAs(bob, http.MethodGet, "/api/account", nil), http.StatusOK, &account)
	if account.LastAdmin {
		t.Error("an ordinary account is reported as the last administrator")
	}

	h.expect(h.doAs(bob, http.MethodPost, "/api/account/deletion",
		map[string]string{"password": harnessPassword}), http.StatusOK, nil)
}

// The message is the safety net for the case this endpoint most needs one — somebody else
// pressing it through a session they should not have.
func TestDeletionWritesToTheRecoveryAddress(t *testing.T) {
	h := newHarness(t)
	relay := h.relay()
	h.signIn(store.RoleAdmin, "ada")
	h.secondAdmin("bob")
	h.confirmRecovery(t, relay, "ada@example.com")

	var body deletionBody
	h.expect(h.schedule(harnessPassword), http.StatusOK, &body)

	if !body.Notified {
		t.Fatal("the answer says nothing was sent")
	}
	sent := relay.lastTo(t, "ada@example.com")
	// The date, and the one thing that undoes it — which the person reading needs to be
	// able to do without asking anybody.
	if !strings.Contains(sent.Body, "Signing in cancels the deletion") {
		t.Errorf("the message does not say how to stop it:\n%s", sent.Body)
	}
	if !strings.Contains(sent.Subject, "erased on") {
		t.Errorf("the subject does not name the date: %q", sent.Subject)
	}
}

// An account with no address on file has no safety net, and the interface says so rather
// than implying the better of the two.
func TestDeletionSaysWhenNothingCouldBeSent(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	h.secondAdmin("bob")

	var body deletionBody
	h.expect(h.schedule(harnessPassword), http.StatusOK, &body)
	if body.Notified {
		t.Error("the answer claims a message went to an account with no address")
	}
}

// The second half: the account and everything in it goes once the week has run out.
func TestThePurgeErasesWhatTheGracePeriodHeld(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	bob := h.signInAsSomebodyElse("bob")

	var body deletionBody
	h.expect(h.doAs(bob, http.MethodPost, "/api/account/deletion",
		map[string]string{"password": harnessPassword}), http.StatusOK, &body)

	target, err := h.store.PrincipalByUsername(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}

	// Nothing yet: the week has not passed.
	erased, err := h.store.PurgeDeletedAccounts(t.Context(), store.DeletionGrace)
	if err != nil {
		t.Fatal(err)
	}
	if len(erased) != 0 {
		t.Fatalf("the purge took %d accounts before the grace period was up", len(erased))
	}

	// And now it has. A grace of zero is the same question asked of a clock a week later.
	erased, err = h.store.PurgeDeletedAccounts(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(erased) != 1 || erased[0].Username != "bob" {
		t.Fatalf("the purge took %+v, want bob", erased)
	}
	if _, err := h.store.PrincipalByID(t.Context(), target.ID); err == nil {
		t.Error("the account is still there after the purge")
	}

	// Everything that hangs off it went too.
	tags, err := h.store.ListTags(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Errorf("%d tags outlived the account", len(tags))
	}
	pages, err := h.store.Pages(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 0 {
		t.Errorf("%d pages outlived the account", len(pages))
	}

	// And the account that did not ask is untouched.
	h.expect(h.do(http.MethodGet, "/api/account", nil), http.StatusOK, nil)
}

// An account that withdrew its request is not taken by a purge that runs afterwards.
func TestThePurgeSparesAWithdrawnRequest(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	bob := h.signInAsSomebodyElse("bob")

	h.expect(h.doAs(bob, http.MethodPost, "/api/account/deletion",
		map[string]string{"password": harnessPassword}), http.StatusOK, nil)
	h.expect(h.doAs(bob, http.MethodPost, "/api/login",
		map[string]string{"username": "bob", "password": harnessPassword}), http.StatusNoContent, nil)

	erased, err := h.store.PurgeDeletedAccounts(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(erased) != 0 {
		t.Fatalf("the purge took %+v after the request was withdrawn", erased)
	}
}

// An administrator should not be surprised by a row vanishing from the list.
func TestAdministratorsSeeAScheduledDeletion(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	bob := h.signInAsSomebodyElse("bob")

	h.expect(h.doAs(bob, http.MethodPost, "/api/account/deletion",
		map[string]string{"password": harnessPassword}), http.StatusOK, nil)

	var users []userBody
	h.expect(h.do(http.MethodGet, "/api/admin/users", nil), http.StatusOK, &users)
	for _, user := range users {
		if user.Username == "bob" {
			if user.DeletedAt == nil {
				t.Error("the list does not say bob asked to be erased")
			}
			return
		}
	}
	t.Fatal("bob is not in the list")
}

func TestDeletionNeedsASession(t *testing.T) {
	h := newHarness(t)
	h.expect(h.do(http.MethodPost, "/api/account/deletion",
		map[string]string{"password": harnessPassword}), http.StatusUnauthorized, nil)
}

// What is erased is what belongs to the person, and a feed does not.
//
// Feeds and the articles in them are the instance's, held once and shared by everybody who
// follows them; what somebody owns is the subscription, the tag they filed it under, the page
// they put it on and whether they have read it. Erasing an account must take the second set
// and leave the first, or one person leaving would take another person's reading with them.
func TestThePurgeLeavesWhatBelongsToEverybody(t *testing.T) {
	h := newHarness(t)
	h.signIn(store.RoleAdmin, "ada")
	bob := h.signInAsSomebodyElse("bob")

	// One feed, followed by both, with articles and a read mark each.
	feed, err := h.store.UpsertFeed(t.Context(), "https://example.com/feed.xml", "The Meridian", "")
	if err != nil {
		t.Fatal(err)
	}
	items := []*store.Item{
		{FeedID: feed.ID, GUID: "one", Title: "Bells", Link: "https://example.com/1",
			PublishedAt: h.store.Now(), FetchedAt: h.store.Now()},
		{FeedID: feed.ID, GUID: "two", Title: "Whistles", Link: "https://example.com/2",
			PublishedAt: h.store.Now().Add(-time.Hour), FetchedAt: h.store.Now()},
	}
	if _, err := h.store.SaveItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}

	ada := h.me(t)
	target, err := h.store.PrincipalByUsername(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ada, target.ID} {
		if _, err := h.store.Subscribe(t.Context(), id, feed.ID, 50, 7*24*time.Hour, nil); err != nil {
			t.Fatal(err)
		}
	}
	// One each, on different articles, so the two read marks cannot be confused for one.
	if err := h.store.SetRead(t.Context(), ada, items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetRead(t.Context(), target.ID, items[1].ID, true); err != nil {
		t.Fatal(err)
	}

	// Ada's front page, composed before bob leaves, so there is something concrete to
	// compare against afterwards.
	var before editionBody
	h.expect(h.do(http.MethodPost, "/api/edition/regenerate", nil), http.StatusOK, &before)

	h.expect(h.doAs(bob, http.MethodPost, "/api/account/deletion",
		map[string]string{"password": harnessPassword}), http.StatusOK, nil)
	if _, err := h.store.PurgeDeletedAccounts(t.Context(), 0); err != nil {
		t.Fatal(err)
	}

	// The composed page ada was reading is the same page, article for article. This is the
	// whole of it: one person leaving is invisible to everybody else.
	var after editionBody
	h.expect(h.do(http.MethodGet, "/api/edition", nil), http.StatusOK, &after)
	if after.ID != before.ID || len(after.Items) != len(before.Items) {
		t.Errorf("ada's page changed when bob left: %s with %d items became %s with %d",
			before.ID, len(before.Items), after.ID, len(after.Items))
	}
	if len(after.Items) == 0 {
		t.Fatal("ada's page was empty to begin with, so this proves nothing")
	}

	// The feed is still here, and so is its article: somebody else follows it.
	if _, err := h.store.FeedByID(t.Context(), feed.ID); err != nil {
		t.Fatalf("the feed went with the account that left: %v", err)
	}
	// And the other person's subscription and read mark are untouched.
	subs, err := h.store.ListSubscriptions(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Errorf("the account that stayed has %d subscriptions, want 1", len(subs))
	}
	read, err := h.store.ReadArticles(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 1 {
		t.Errorf("the account that stayed has %d read articles, want 1", len(read))
	}

	// What the erased account had is gone.
	gone, err := h.store.ReadArticles(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Errorf("%d read marks outlived the account", len(gone))
	}
}
