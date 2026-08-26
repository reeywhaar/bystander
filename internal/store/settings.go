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

// ValidArticleWindow reports whether a duration is one of the reaches this program offers.
//
// Exported because an import has to decide what to do with a number somebody else's file
// named, and "not one of ours" is a different answer from "invalid" — it takes the default
// rather than refusing the feed.
func ValidArticleWindow(d time.Duration) bool { return validWindow(d) }

func validWindow(d time.Duration) bool {
	for _, valid := range ArticleWindows {
		if d == valid {
			return true
		}
	}
	return false
}

// ItemRetention is how long one feed's articles are kept.
type ItemRetention struct {
	// Forever is set when somebody following this feed asked to reach back without limit.
	//
	// Then nothing prunes it by age at all, and [MaxItemsPerFeed] is the only thing that
	// bounds it. A ceiling in years would not do: how far back a page reaches is a bound on
	// when an article was *published*, and what prunes goes by when it was *fetched*, so
	// capping the second at a year quietly makes the first untrue. A feed whose every
	// article was written two years ago is a perfectly ordinary thing to want all of —
	// an archive, a comic's back catalogue, a blog that stopped — and under a ceiling its
	// articles would be dropped a year after they were first seen and, if the publisher had
	// moved them out of the document by then, never come back.
	Forever bool
	// For is how long, when Forever is false. Never less than [MinItemRetention].
	For time.Duration
}

// ItemRetentionByFeed is how long each feed's articles are kept, given what the people who
// follow *that feed* asked to see.
//
// Per feed, because one number for the whole instance is one number too few. Retention was
// instance-wide and took the longest window anybody had chosen anywhere, which meant a
// webcomic somebody wanted a year of made a news feed at ninety articles a day keep a year
// as well — thirty thousand articles nobody had asked for, to serve a page that shows sixty.
// The windows are per subscription precisely because a news feed worth a day and a blog
// worth a year are the pair one number cannot serve, and the pruning has to agree with that
// or the setting is only half real.
//
// The most demanding follower wins, and "no limit" is the most demanding of all. A feed
// nobody follows is absent rather than zero: it constrains nothing, and everything it has is
// due to be collected.
func (s *Store) ItemRetentionByFeed(ctx context.Context) (map[string]ItemRetention, error) {
	rows, err := s.main.QueryContext(ctx,
		// min() finds the zero if there is one, which is how "no limit" wins over any
		// number; max() is the longest of the rest.
		`SELECT feed_id, min(max_article_age) = 0, max(max_article_age)
		   FROM subscriptions GROUP BY feed_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]ItemRetention{}
	for rows.Next() {
		var (
			feedID    string
			unlimited bool
			seconds   int64
		)
		if err := rows.Scan(&feedID, &unlimited, &seconds); err != nil {
			return nil, err
		}
		if unlimited {
			out[feedID] = ItemRetention{Forever: true}
			continue
		}
		out[feedID] = ItemRetention{For: atLeastFloor(time.Duration(seconds) * time.Second)}
	}
	return out, rows.Err()
}

// atLeastFloor holds a window to the thirty days below which a front page has too little to
// draw from. There is no ceiling here: a window of a year means a year.
func atLeastFloor(d time.Duration) time.Duration {
	if d < MinItemRetention {
		return MinItemRetention
	}
	return d
}
