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
	Payload   string
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
func (s *Store) Enqueue(ctx context.Context, kind, key, payload string) error {
	now := s.Now()
	_, err := s.main.ExecContext(ctx, `
		INSERT INTO jobs (id, kind, payload, unique_key, attempts, run_at, created_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT (unique_key) DO NOTHING`,
		ids.New(ids.Job), kind, payload, key, now.Unix(), now.Unix())
	return err
}

// DueJobs are the jobs ready to run, oldest first.
func (s *Store) DueJobs(ctx context.Context, limit int) ([]*Job, error) {
	rows, err := s.main.QueryContext(ctx, `
		SELECT id, kind, payload, attempts, run_at, created_at, last_error
		  FROM jobs WHERE run_at <= ? ORDER BY run_at LIMIT ?`,
		s.Now().Unix(), limit)
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
		if err := rows.Scan(&job.ID, &job.Kind, &job.Payload, &job.Attempts,
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
func (s *Store) QueueDepth(ctx context.Context) (int, error) {
	var n int
	err := s.main.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&n)
	return n, err
}
