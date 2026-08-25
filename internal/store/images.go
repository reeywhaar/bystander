package store

import (
	"context"
	"strings"
	"time"
)

// SetImageSize records what a picture turned out to be.
//
// Written against the URL rather than one article, so a single measurement answers for every
// article that shares the picture — which is how a publisher's standing illustration stops
// being asked about once per article that uses it.
//
// Nothing is written to image_retry_at, because the size is what ends the asking: a picture
// with dimensions is never offered again however long ago it answered.
func (s *Store) SetImageSize(ctx context.Context, url string, width, height int) error {
	if width <= 0 || height <= 0 {
		return Invalid("a picture cannot be %dx%d", width, height)
	}
	_, err := s.derived.ExecContext(ctx,
		`UPDATE items SET image_width = ?, image_height = ?, image_error = ''
		  WHERE image_url = ?`, width, height, url)
	return err
}

// PostponeImage puts a picture that could not be measured out of reach for a while.
//
// For a while, and not for good — which is the whole of what this used to get wrong. It was a
// flag called "probed", set on any failure, and the queue only ever offered pictures that had
// never been asked. One timeout at a CDN cost that picture its size permanently; fifteen of the
// nineteen pictures on a real comics page were stuck exactly that way, and every one of them
// measured on the first try when finally asked again.
//
// How long is the caller's to decide, because only the caller knows what happened: a server
// that said it was having trouble has said "not now", and one that said the picture is gone has
// said something more settled. Neither is permanent — a 404 today is a picture that moved, and
// an undecodable format today is a format nothing had a decoder for until somebody added one.
//
// reason is that same knowledge kept where a later version can use it. A category rather than a
// message: when a decoder is added for a format there was none for, a migration can re-offer
// every picture that failed for exactly that reason and nothing else.
func (s *Store) PostponeImage(ctx context.Context, url, reason string, after time.Duration) error {
	_, err := s.derived.ExecContext(ctx,
		`UPDATE items SET image_retry_at = ?, image_error = ? WHERE image_url = ?`,
		unix(s.Now().Add(after)), reason, url)
	return err
}

// UnmeasuredImages are the pictures still without a size that are due to be asked about again,
// newest first.
//
// The size is what decides it, not the asking: a picture that answered is never offered again,
// and a picture that did not is offered once its postponement has run out.
//
// One per distinct URL: publishers reuse a picture across articles, and asking a host the same
// question a dozen times is the rudeness this queue is arranged to avoid.
func (s *Store) UnmeasuredImages(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.derived.QueryContext(ctx, `
		SELECT image_url FROM items
		 WHERE image_url <> ''
		   AND (image_width <= 0 OR image_height <= 0)
		   AND image_retry_at <= ?
		 GROUP BY image_url
		 ORDER BY max(published_at) DESC
		 LIMIT ?`, unix(s.Now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		out = append(out, url)
	}
	return out, rows.Err()
}

// ImageFailures is why each unmeasured picture is unmeasured, keyed by URL.
//
// For the operator, and for the migration that acts on it: a build that gains a decoder for a
// format it could not read re-offers the pictures that failed as undecodable and leaves the
// rest alone. Reading it is also the only way to find out that, say, every failure on an
// instance is one host refusing hotlinks.
func (s *Store) ImageFailures(ctx context.Context, limit int) (map[string]string, error) {
	rows, err := s.derived.QueryContext(ctx, `
		SELECT image_url, image_error FROM items
		 WHERE image_url <> '' AND image_error <> ''
		   AND (image_width <= 0 OR image_height <= 0)
		 GROUP BY image_url
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var url, reason string
		if err := rows.Scan(&url, &reason); err != nil {
			return nil, err
		}
		out[url] = reason
	}
	return out, rows.Err()
}

// UnmeasuredImage is one picture without a size, for the list behind a reason.
//
// The URL is the identity — a picture is measured once for every article that uses it — and
// Articles says how many of them are waiting on it, which is what makes one row worth more
// attention than another. Title is one of those articles, because a bare CDN address says
// nothing about which publisher an operator is looking at.
type UnmeasuredImage struct {
	URL      string
	Reason   string
	RetryAt  time.Time
	Articles int
	Title    string
}

// UnmeasuredByReason lists the pictures behind one of Images's counts.
//
// The counts alone answer "what is wrong"; this answers "with what", which is the question
// anybody who has read the counts asks next. One host refusing hotlinks and forty publishers
// each losing one picture are the same number under "refused" and not remotely the same
// afternoon — and the addresses are what tell them apart.
//
// reason is matched exactly, empty included: nothing has asked about those yet, which is a
// group somebody looks at for the same reason as the others.
//
// Most-used first, so the picture the most articles are waiting on is the one at the top. The
// limit is a ceiling on a screen rather than a page of results: the count beside the reason
// already says how many there are, and a list long enough to need paging is a list whose
// answer was the count.
func (s *Store) UnmeasuredByReason(ctx context.Context, reason string, limit int) ([]UnmeasuredImage, error) {
	// max(published_at) with bare columns beside it: SQLite takes those from the row that
	// matched the aggregate, so the title is the newest article using this picture rather
	// than whichever row the group happened to start with.
	rows, err := s.derived.QueryContext(ctx, `
		SELECT image_url, image_error, max(image_retry_at), count(*), title, max(published_at)
		  FROM items
		 WHERE image_url <> '' AND image_error = ?
		   AND (image_width <= 0 OR image_height <= 0)
		 GROUP BY image_url
		 ORDER BY count(*) DESC, image_url
		 LIMIT ?`, reason, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []UnmeasuredImage{}
	for rows.Next() {
		var (
			pic       UnmeasuredImage
			retryAt   int64
			published int64
		)
		if err := rows.Scan(&pic.URL, &pic.Reason, &retryAt, &pic.Articles, &pic.Title, &published); err != nil {
			return nil, err
		}
		if retryAt > 0 {
			pic.RetryAt = time.Unix(retryAt, 0).UTC()
		}
		out = append(out, pic)
	}
	return out, rows.Err()
}

// RetryImage offers one picture back to the queue, and says how many articles it unblocks.
//
// The single-picture half of RetryImages, and it earns being separate: the reason is a whole
// category and this is one address, so a caller that could pass either would be a caller that
// can pass both and mean something nobody decided.
func (s *Store) RetryImage(ctx context.Context, url string) (int, error) {
	if strings.TrimSpace(url) == "" {
		return 0, Invalid("which picture?")
	}
	res, err := s.derived.ExecContext(ctx, `
		UPDATE items SET image_retry_at = 0
		 WHERE image_url = ? AND (image_width <= 0 OR image_height <= 0)`, url)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// RetryImages offers unmeasured pictures again straight away, and says how many.
//
// reason narrows it to one category — see the values PostponeImage is given — and an empty one
// means every picture still without a size. Pictures that were measured are never touched: the
// size is what ends the asking, and nothing here is asking for it to start again.
//
// This is the manual half of the same idea the retry window automates. The window is for a host
// that was having a moment; this is for the times the *program* was wrong — a decoder it did not
// have, a header it did not send, an address it resolved badly — where waiting out a day per
// picture is a slow way to find out something you already know.
func (s *Store) RetryImages(ctx context.Context, reason string) (int, error) {
	res, err := s.derived.ExecContext(ctx, `
		UPDATE items SET image_retry_at = 0
		 WHERE image_url <> ''
		   AND (image_width <= 0 OR image_height <= 0)
		   AND (? = '' OR image_error = ?)`, reason, reason)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ImageTally is how the pictures on an instance are getting on.
type ImageTally struct {
	// Pictures is how many distinct pictures are held at all. Distinct, because publishers
	// reuse one across articles and it is measured once for all of them.
	Pictures int
	Measured int
	// Failures is how many are unmeasured, by the reason recorded against them. Pictures
	// nothing has asked about yet are counted under an empty reason.
	Failures map[string]int
}

// Unmeasured is every picture still without a size, whatever the reason.
func (t ImageTally) Unmeasured() int {
	var n int
	for _, count := range t.Failures {
		n += count
	}
	return n
}

// Images counts the pictures and why the unmeasured ones are unmeasured.
//
// For a screen an administrator looks at when a page is drawing shapes instead of pictures.
// The number that matters is not the total but the breakdown: a hundred failures that all say
// "refused" is one host with hotlink protection, and a hundred that say "undecodable" is a
// format this build cannot read — two very different afternoons.
func (s *Store) Images(ctx context.Context) (ImageTally, error) {
	tally := ImageTally{Failures: map[string]int{}}

	err := s.derived.QueryRowContext(ctx, `
		SELECT count(DISTINCT image_url),
		       count(DISTINCT CASE WHEN image_width > 0 AND image_height > 0 THEN image_url END)
		  FROM items WHERE image_url <> ''`).Scan(&tally.Pictures, &tally.Measured)
	if err != nil {
		return tally, err
	}

	rows, err := s.derived.QueryContext(ctx, `
		SELECT image_error, count(DISTINCT image_url) FROM items
		 WHERE image_url <> '' AND (image_width <= 0 OR image_height <= 0)
		 GROUP BY image_error`)
	if err != nil {
		return tally, err
	}
	defer rows.Close()

	for rows.Next() {
		var reason string
		var count int
		if err := rows.Scan(&reason, &count); err != nil {
			return tally, err
		}
		tally.Failures[reason] = count
	}
	return tally, rows.Err()
}
