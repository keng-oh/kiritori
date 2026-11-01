// Package migrations embeds the SQL migrations so they ship inside the binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
