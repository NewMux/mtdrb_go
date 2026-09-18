package db

import "embed"

// MigrationsDir is the path of the migration set inside Migrations.
const MigrationsDir = "migrations"

// Migrations holds the SQL migration set, embedded so that the migrate binary
// is self-contained and cannot drift from the code it ships with.
//
//go:embed migrations/*.sql
var Migrations embed.FS
