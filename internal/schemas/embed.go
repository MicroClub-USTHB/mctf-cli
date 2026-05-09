// Package schemas embeds the JSON Schemas that ship with the CLI.
//
// At runtime the CLI uses these to validate user-edited and derived files.
// They are the same files in /schema/ at the repo root — embedded here so
// the binary is self-contained.
package schemas

import _ "embed"

//go:embed challenge.schema.json
var Challenge []byte

//go:embed mtcf-config.schema.json
var MCTFConfig []byte

//go:embed ctfd-manifest.schema.json
var CTFdManifest []byte

//go:embed instance.schema.json
var Instance []byte
