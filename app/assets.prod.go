//go:build !dev

package app

import (
	"embed"
	"net/http"
)

const Embedded = true

//go:embed *.js
//go:embed *.css
//go:embed *.html
var content embed.FS

// FS provides a filesystem for static webapp assets
// that are embedded into the production binary.
var FS http.FileSystem = http.FS(content)
