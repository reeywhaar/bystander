package migrations

// Whether "/" greets a stranger with a landing page or with the login form.
//
// On, and the only switch on this row that starts that way. The other two are exposure — who
// may put a page on the open web, and whether a search engine may keep it — and an exposure
// switch that arrived on would be a decision made on somebody's behalf. This one is not
// exposure: it decides what the front door *says*, and an instance whose front door explains
// itself is the better default. An operator who wants a bare login form is making a choice
// about their own instance and can make it.
var mainLandingPage = Migration{
	Name: "20260826005150_main_landing_page",
	Up: exec(`
ALTER TABLE instance_settings ADD COLUMN landing INTEGER NOT NULL DEFAULT 1 CHECK (landing IN (0, 1));
`),
}
