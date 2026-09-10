// Package web holds the embedded browser UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// FS returns the UI files with index.html at the root.
func FS() fs.FS {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // the embedded tree always has a static directory
	}

	return sub
}
