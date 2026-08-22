// Package web holds the embedded UI assets: html/template sources under
// templates/ and the single stylesheet under static/.
package web

import "embed"

//go:embed templates static
var Files embed.FS
