// Package scaffold renders embedded templates onto the filesystem to
// bootstrap a new MTCF event repository.
//
// Files are embedded under templates/event/. Names ending in ".tmpl" are
// run through text/template; all other files are copied verbatim. Two
// files are renamed on write so they survive embedding (Go's embed
// strips files starting with "." or "_" from directory listings only
// for "." prefixes; we use "_" for clarity):
//
//   _gitignore   -> .gitignore
//   _env.example -> .env.example
package scaffold

import "embed"

//go:embed all:templates/event
var eventFS embed.FS
