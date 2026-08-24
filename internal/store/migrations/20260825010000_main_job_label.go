package migrations

// What a job is about, in words, and the index that now chooses it.
//
// Two changes for one reason: jobs stopped being one queue.
//
// **The label.** The queue keeps a kind and an opaque payload, which is right — a column per
// kind of work would make every new kind a migration. But it leaves nothing readable: the only
// human-facing string was unique_key, and that is identity rather than description. It reads
// well enough for a picture, whose identity is its URL, and not at all for a feed, whose
// identity is a ULID. So the enqueuer says what the job is about, once, and every log line and
// every future queue screen has something to show besides an opaque id.
//
// Written at enqueue rather than worked out at display time, because the thing best placed to
// describe a job is whatever created it, and by the time anybody is reading the queue the feed
// may have been renamed or unfollowed.
//
// **The index.** Work is now chosen per kind — each kind has its own cadence, its own width,
// and its own loop — so the query gained a `kind = ?` that the old index on run_at alone could
// not serve. With one kind that was a full scan of a short table and nobody would have noticed;
// with pictures outnumbering feeds by a hundred to one, a feed sweep would scan every queued
// picture to find nothing.
var mainJobLabel = Migration{
	Name: "20260825010000_main_job_label",
	Up: exec(`
ALTER TABLE jobs ADD COLUMN label TEXT NOT NULL DEFAULT '';

DROP INDEX jobs_due;
CREATE INDEX jobs_due ON jobs(kind, run_at);
`),
}
