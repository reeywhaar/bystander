package store

import "context"

// SetImageSize records what a picture turned out to be.
//
// Written against the URL rather than one article, so a single measurement answers for every
// article that shares the picture — which is how a publisher's standing illustration stops
// being asked about once per article that uses it.
func (s *Store) SetImageSize(ctx context.Context, url string, width, height int) error {
	if width <= 0 || height <= 0 {
		return Invalid("a picture cannot be %dx%d", width, height)
	}
	_, err := s.derived.ExecContext(ctx,
		`UPDATE items SET image_width = ?, image_height = ? WHERE image_url = ?`,
		width, height, url)
	return err
}

// UnmeasuredImages are the pictures nothing knows the size of yet, newest first.
//
// One per distinct URL: publishers reuse a picture across articles, and enqueueing the same
// measurement a dozen times would be a dozen requests to a host that only needs one.
func (s *Store) UnmeasuredImages(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.derived.QueryContext(ctx, `
		SELECT image_url FROM items
		 WHERE image_url <> '' AND image_width = 0
		 GROUP BY image_url
		 ORDER BY max(published_at) DESC
		 LIMIT ?`, limit)
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
