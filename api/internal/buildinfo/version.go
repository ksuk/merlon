// Package buildinfo exposes metadata that must identify the same build across
// every Merlon command and generated contract.
package buildinfo

// Version is "dev" for unversioned local builds. Release, container, and
// documentation entry points override it with -ldflags -X.
var Version = "dev"
