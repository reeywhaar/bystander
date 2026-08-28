package migrations

// Why somebody follows a feed, in their own words.
//
// A list of forty feeds is forty names, and a name is the one thing about a feed that was
// chosen by somebody else. "Simon Willison" says who writes it and nothing about why it is
// here; "Import AI" says nothing at all. The answer to "why am I still reading this" is
// knowledge that exists only in the person's head, and a feed they stop being able to place
// is a feed they will not unfollow either, because unfollowing it might be a mistake.
//
// On the subscription rather than on the feed, because it is a note to self and not a fact
// about the publisher. Two people following the same feed have different reasons, and one of
// them writing theirs down must not put it in front of the other. The feed already carries the
// publisher's own description of itself if anybody wants that; this is the other thing.
//
// Empty by default and empty for almost every row, which is the shape it should have: a note
// is worth having because it was written deliberately about the few feeds that needed one.
var mainSubscriptionNote = Migration{
	Name: "20260828052421_main_subscription_note",
	Up: exec(`
ALTER TABLE subscriptions ADD COLUMN note TEXT NOT NULL DEFAULT '';
`),
}
