package migrations

// The one page everybody has is called the Front Page.
//
// It was "Your page", which the migration that created pages wrote into every row. That was
// never a name — it read as a label on a settings screen — and once a person could have several
// it stopped being true as well, since they are all your pages.
//
// Only the rows that still say the old thing, and only main pages. Somebody who has renamed
// theirs cannot have, since the main page's name is fixed, but the condition costs nothing and
// says what is meant: this corrects a default, it does not overwrite a choice.
var mainFrontPageName = Migration{
	Name: "20260824050000_main_front_page_name",
	Up: exec(`
UPDATE pages SET name = 'Front Page' WHERE is_main = 1 AND name = 'Your page';
`),
}
