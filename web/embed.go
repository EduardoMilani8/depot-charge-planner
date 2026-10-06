// Package web embeds the lab's static files.
package web

import "embed"

// Static holds web/static: index.html, app.css and the JavaScript modules.
//
//go:embed static
var Static embed.FS
