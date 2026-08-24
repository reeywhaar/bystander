package migrations

// Work to be done later, and the record that it has not been done yet.
//
// Fetching a feed happens on a schedule and is over in a second. Everything else this program
// might want to do in the background — measuring a picture, sending a mail nobody is waiting
// on, whatever comes next — shares one shape: it can fail, it should be retried a few times
// and then left alone, and it must survive a restart. That is a queue, and writing it once is
// cheaper than each of those growing its own attempts column and its own backoff.
//
// In main.db, not derived.db. derived is the disposable half and is rebuilt from the feeds; a
// queue is not rebuildable from anything, and work silently lost because a cache was rebuilt is
// the failure mode a queue exists to prevent. A job whose subject has since been pruned is a
// job its handler drops, which is cheap and obvious.
//
// The payload is JSON, and deliberately opaque here: the table knows a job has a kind and some
// bytes, and the handler registered for that kind knows what the bytes mean. A column per kind
// of work would make every new kind a migration.
var mainJobs = Migration{
	Name: "20260824013604_main_jobs",
	Up: exec(`
CREATE TABLE jobs (
    id         TEXT    NOT NULL PRIMARY KEY,
    -- Which handler runs it.
    kind       TEXT    NOT NULL,
    payload    TEXT    NOT NULL,
    -- What makes this job the same job as that one.
    --
    -- Unique, so enqueueing the same work twice is one row rather than two. Publishers reuse a
    -- picture across a dozen articles, and asking a host the same question a dozen times is the
    -- rudeness this whole queue is arranged to avoid.
    unique_key TEXT    NOT NULL UNIQUE,
    attempts   INTEGER NOT NULL DEFAULT 0,
    -- The earliest this should be tried. Set ahead on every failure, which is the backoff.
    run_at     INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    -- Kept for the operator rather than for the runner: a queue that will not drain is a
    -- question somebody has to answer, and "it failed" is not an answer.
    last_error TEXT    NOT NULL DEFAULT ''
) STRICT;

-- The whole of how work is chosen: what is due, oldest first.
CREATE INDEX jobs_due ON jobs(run_at);
`),
}
