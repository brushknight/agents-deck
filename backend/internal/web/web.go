// Package web embeds the dashboard's static files.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var files embed.FS

func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
