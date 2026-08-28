// Package library embeds the bash-teacher content set into the binary.
//
// It deliberately contains no logic: internal/content owns parsing and
// validation, and takes an fs.FS so tests can substitute a fixture library.
package library

import "embed"

//go:embed commands exercises cards fixtures expected
var files embed.FS

// FS is the embedded content tree, rooted at the content directory.
var FS embed.FS = files
