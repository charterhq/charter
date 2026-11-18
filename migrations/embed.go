// Package migrations embeds the SQL schema of the Charter core.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
