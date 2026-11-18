// Package schemas embeds the JSON Schemas of the Charter v1 package format.
package schemas

import "embed"

//go:embed *.schema.json
var FS embed.FS
