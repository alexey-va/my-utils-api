package migrations

import "embed"

// FS contains the immutable SQL migrations that were previously applied by Flyway.
//
//go:embed *.sql
var FS embed.FS
