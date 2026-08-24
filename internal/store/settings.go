package store

import (
	"context"
	"time"
)

// The intervals an edition can be generated on.
//
// A closed set rather than an arbitrary schedule: a cron expression is a support burden
// with no matching demand, and four options fit in a segmented control. The CHECK
// constraint in the schema holds the same list, so a value that got past this would still
// be refused by the database.
var EditionIntervals = []time.Duration{
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
	7 * 24 * time.Hour,
}

// Bounds on how many articles a page holds.
const (
	MinEditionSize = 10
	MaxEditionSize = 200
)

// ArticleWindows are how recent an article must be to reach a page, in the order the
// interface offers them. Zero is no limit.
//
// A closed set for the same reason the intervals are: four or five choices fit in a
// control, and an arbitrary duration is a support burden nobody asked for. The schema
// holds the same list in a CHECK, so a value that got past this would still be refused.
var ArticleWindows = []time.Duration{
	0,
	365 * 24 * time.Hour,
	30 * 24 * time.Hour,
	14 * 24 * time.Hour,
	7 * 24 * time.Hour,
	24 * time.Hour,
}

// DefaultArticleWindow is a week. A front page is about what is going on, and a
// fortnight-old article on it is a different kind of object.
const DefaultArticleWindow = 7 * 24 * time.Hour

func validInterval(d time.Duration) bool {
	for _, valid := range EditionIntervals {
		if d == valid {
			return true
		}
	}
	return false
}

func validWindow(d time.Duration) bool {
	for _, valid := range ArticleWindows {
		if d == valid {
			return true
		}
	}
	return false
}

// EffectiveItemRetention is how long articles are kept, given what people have asked to
// see.
//
// Retention used to be a flat thirty days, which quietly made the longer windows a lie: a
// page set to show a year of articles would have had nothing older than a month to show.
// So the floor stays at thirty days and the ceiling at a year, and between them it follows
// whoever wants to see furthest back.
//
// "No limit" maps to the ceiling rather than to forever. Unbounded growth is not a setting
// anybody meant to choose, and a year is far past the point where a front page is about
// what is going on.
func (s *Store) EffectiveItemRetention(ctx context.Context) (time.Duration, error) {
	var longest int64
	err := s.main.QueryRowContext(ctx,
		// A zero anywhere means some feed was asked to reach back without limit, so it
		// takes the ceiling. Read from subscriptions, because that is where the window
		// lives: a feed nobody follows any more constrains nothing.
		`SELECT CASE WHEN count(*) = 0 THEN ?
		             WHEN min(max_article_age) = 0 THEN ?
		             ELSE max(max_article_age) END
		   FROM subscriptions`,
		int64(MinItemRetention.Seconds()), int64(MaxItemRetention.Seconds())).Scan(&longest)
	if err != nil {
		return MinItemRetention, err
	}

	retention := time.Duration(longest) * time.Second
	if retention < MinItemRetention {
		return MinItemRetention, nil
	}
	if retention > MaxItemRetention {
		return MaxItemRetention, nil
	}
	return retention, nil
}
