// Package migrations embeds the SQL schema shared by every store.
package migrations

import _ "embed"

// Schema is the idempotent DDL for workspaces, actions and variables.
//
//go:embed schema.sql
var Schema string
