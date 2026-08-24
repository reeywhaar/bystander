package migrations

// Whether a picture has been asked about, as distinct from whether the answer was useful.
//
// Zero width means unmeasured, which was doing two jobs: "nobody has asked yet" and "somebody
// asked and got nothing". The queue is a query for unmeasured pictures, so the second kind
// came back every time it ran — a picture that 404s would have been fetched again, and again,
// for as long as the article existed. Politeness was the entire point of the queue, and that
// undid it.
//
// **Asked once, and that is the end of it.** Not asked once and retried tomorrow: a picture
// nothing could measure costs nothing, because the page draws a shape for it and looks exactly
// as it does today. There is no outcome here worth a second request to somebody else's server.
//
// The cost is that a host which happens to be down during its one moment is never measured.
// That is a page that looks like the page already looks, which is the reason it is affordable.
var derivedImageProbed = Migration{
	Name: "20260824015002_derived_image_probed",
	Up: exec(`
ALTER TABLE items ADD COLUMN image_probed INTEGER NOT NULL DEFAULT 0;
`),
}
