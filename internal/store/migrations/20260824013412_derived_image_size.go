package migrations

// How big an article's picture actually is.
//
// The page crops every picture to one of five shapes drawn from the article's id, which is a
// reasonable thing to do knowing nothing — but it is knowing nothing. A photograph that is
// nearly square cut to five-by-three loses a third of itself, and nobody here has looked at it
// to know whether the third mattered.
//
// Zero means unmeasured, and unmeasured is the ordinary case rather than a failure: the page
// falls back to the drawn shape and looks exactly as it does now. Everything this enables is an
// improvement to a page that already works without it.
//
// Nothing about *scheduling* the measurement lives here. That is a job, and jobs have their own
// table in main.db — see the jobs migration and internal/jobs. An earlier draft of this carried
// its own attempts and backoff columns and a partial index to serve as the queue, which worked
// and would have gone on working right up until the second kind of background work needed the
// same machinery and had to invent it again.
var derivedImageSize = Migration{
	Name: "20260824013412_derived_image_size",
	Up: exec(`
ALTER TABLE items ADD COLUMN image_width  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN image_height INTEGER NOT NULL DEFAULT 0;
`),
}
