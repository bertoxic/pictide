// Package public embeds the browser assets into the server binary.
package public

import "embed"

//go:embed *.html *.js *.css
var Files embed.FS
