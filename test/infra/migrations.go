package infra

import _ "embed"

//go:embed ../migrations/000_all.sql
var MigrationsAll string
