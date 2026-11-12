package migrations

import "embed"

// Files contains the ordered, immutable SQLite migrations.
//
//go:embed *.sql
var Files embed.FS
