// Package app holds what this program is, as distinct from what it was told.
//
// Four facts, and the line between this package and internal/config is what decides
// whether something belongs here: config is the environment — things an operator chose and
// can change without rebuilding. These are properties of the binary itself. An operator
// cannot make bystander listen on 8080 any more than they can make it a different project.
//
// It deliberately does not become a home for every constant in the program. Priorities,
// retentions and the session cookie's name are domain rules, and they belong beside the
// code that enforces them — a `constants` package that collects them is a package with no
// subject, and the first thing anybody does with one is stop reading it.
package app

// Name is what this program is called: in its User-Agent, and in an operator's logs.
const Name = "bystander"

// ProjectURL is where somebody wondering what bystander is can go and read it.
//
// It rides in the User-Agent alongside the instance's own address: the project link says
// what the software is, the instance link says who to talk to about this particular one. A
// publisher looking at their logs wants both, and giving them neither is how a fetcher ends
// up blocked rather than merely rate-limited.
const ProjectURL = "https://github.com/reeywhaar/bystander"

// ListenAddr is where serve listens, inside the container. Not configurable — remap it
// with `docker run -p 8080:80`. A port number inside a container is not a thing an operator
// should have to think about twice.
const ListenAddr = ":80"

// BackupListenAddr is where GET /backup serves a tgz of the databases, on a listener of its
// own. Fixed for the same reason ListenAddr is: a port number inside a container is not a
// thing an operator should have to think about twice.
//
// A second listener rather than a route on the first, and that is the security model, because
// there is no other one — the route is unauthenticated. It is meant for a sibling container on
// a private network, and separate listeners make "not exposed" a property of the deployment
// rather than of a middleware being right: publishing the reader with `-p 8080:80` cannot
// reach it, and the mistake that would is `-p 3000:3000`, which nobody writes by accident.
const BackupListenAddr = ":3000"

// Version is the build this is, stamped at link time:
//
//	-ldflags "-X bystander/internal/app.Version=$(git rev-parse --short HEAD)"
//
// A variable rather than a constant for exactly that reason, and the only one in this
// package. "dev" is what a local build says, and it is true.
var Version = "dev"
