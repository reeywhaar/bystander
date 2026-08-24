package store

import (
	"context"
	"time"

	"bystander/internal/ids"
)

// Job is one piece of background work, as stored.
type Job struct {
	ID   string
	Kind string
	// Payload is whatever the handler for this kind wrote. Opaque here on purpose.
	Payload string
	// Label is what this job is about, in words, for a log line or a queue screen.
	Label     string
	Attempts  int
	RunAt     time.Time
	CreatedAt time.Time
	LastError string
}

// Enqueue adds work, or leaves the existing row alone if the same work is already queued.
//
// key is what makes two jobs the same job. A picture used by a dozen articles is one
// measurement, not twelve, and it is the caller's business to say so — the queue cannot know
// which fields of a payload are identity and which are detail.
//
// Existing means existing, not "existing and due". A job that has failed twice and is waiting
// out its backoff must not be reset to now by something enqueueing it again, or a feed that
// refetches every hour would retry a dead host every hour for ever.
// label is the same work described for a person — a picture's URL, a feed's title. Opaque to
// the queue, which never reads it, and the only readable thing about a job: the payload is
// deliberately unreadable here and unique_key is identity rather than description.
func (s *Store) Enqueue(ctx context.Context, kind, key, label, payload string) error {
	now := s.Now()
	_, err := s.main.ExecContext(ctx, `
		INSERT INTO jobs (id, kind, payload, label, unique_key, attempts, run_at, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT (unique_key) DO NOTHING`,
		ids.New(ids.Job), kind, payload, label, key, now.Unix(), now.Unix())
	return err
}

// DueJobs are the jobs of one kind that are ready to run, oldest first.
//
// Per kind, because each kind is worked by its own loop at its own pace: measuring pictures is
// one every three seconds because the constraint is politeness to a host, and fetching feeds is
// six at a time every minute because the constraint is not being slower than publishing. A
// single queue could only have had one answer, and it would have been wrong for one of them.
func (s *Store) DueJobs(ctx context.Context, kind string, limit int) ([]*Job, error) {
	rows, err := s.main.QueryContext(ctx, `
		SELECT id, kind, payload, label, attempts, run_at, created_at, last_error
		  FROM jobs WHERE kind = ? AND run_at <= ? ORDER BY run_at LIMIT ?`,
		kind, s.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Job
	for rows.Next() {
		var (
			job     Job
			runAt   int64
			created int64
		)
		if err := rows.Scan(&job.ID, &job.Kind, &job.Payload, &job.Label, &job.Attempts,
			&runAt, &created, &job.LastError); err != nil {
			return nil, err
		}
		job.RunAt = time.Unix(runAt, 0).UTC()
		job.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, &job)
	}
	return out, rows.Err()
}

// FinishJob removes a job. Done, or given up on — the queue does not distinguish, because
// neither is coming back.
func (s *Store) FinishJob(ctx context.Context, id string) error {
	_, err := s.main.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	return err
}

// RetryJob puts a job back, later, and remembers why.
func (s *Store) RetryJob(ctx context.Context, id string, after time.Duration, reason string) error {
	// Truncated, because this is a note for whoever is reading the queue and some hosts
	// answer failures with an entire HTML page.
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := s.main.ExecContext(ctx, `
		UPDATE jobs
		   SET attempts = attempts + 1, run_at = ?, last_error = ?
		 WHERE id = ?`, s.Now().Add(after).Unix(), reason, id)
	return err
}

// QueueDepth is how much work is waiting, for the log line that says whether it is draining.
//
// An empty kind means every kind, which is what the line at startup wants: one number saying
// whether this instance came back to anything.
func (s *Store) QueueDepth(ctx context.Context, kind string) (int, error) {
	var n int
	err := s.main.QueryRowContext(ctx,
		`SELECT count(*) FROM jobs WHERE ? = '' OR kind = ?`, kind, kind).Scan(&n)
	return n, err
}

// QueuedKinds is every kind of work currently in the queue.
//
// For the line at startup that says the queue holds something nothing here can run, which is
// what a downgrade or a half-finished deploy looks like. Those rows are left alone rather than
// dropped — throwing them away would be losing work — so the only other symptom is a number
// that never goes down.
func (s *Store) QueuedKinds(ctx context.Context) ([]string, error) {
	rows, err := s.main.QueryContext(ctx, `SELECT DISTINCT kind FROM jobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		out = append(out, kind)
	}
	return out, rows.Err()
}
