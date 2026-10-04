package migrations

// Whether a saved article has been read on the page of saved articles, apart from read_articles.
//
// The two pages are read for different reasons, and one mark between them meant each undid the
// other: saving reads an article where it was found, and that greyed it on arrival on the page it
// was saved to; reading it there greyed it back on the Front Page. Its own column, on the save,
// so the mark is made and dropped with the save itself. NULL is unread.
var mainSavedRead = Migration{
	Name: "20261004113417_main_saved_read",
	Up: exec(`
ALTER TABLE saved ADD COLUMN read_at INTEGER;
`),
}
