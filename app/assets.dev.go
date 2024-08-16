//go:build dev

package app

import "net/http"

const Embedded = false

// FS provides a filesystem for static webapp assets
// so that you can edit them without restarting the app.
var FS http.FileSystem = http.Dir("app")
