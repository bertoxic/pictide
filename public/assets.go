// Package public embeds the browser assets into the server binary.
package public

import "embed"

//go:embed *.html *.js *.css *.png *.svg
var Files embed.FS
