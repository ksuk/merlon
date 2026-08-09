// Package buildinfo exposes metadata that must identify the same build across
// every Merlon command and generated contract.
package buildinfo

// Version is "dev" for unversioned local builds. Release, container, and
// documentation entry points override it with -ldflags -X.
var Version = "dev"

// Commit and BuiltAt identify which build produced a record. They are empty
// unless the build sets them, and empty is reported as empty: a placeholder
// would let an operator believe provenance was captured when it was not.
var (
	Commit  = ""
	BuiltAt = ""
)
