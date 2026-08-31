// Package guidanceassets embeds deterministic guidance catalogs and packs.
package guidanceassets

import "embed"

// Files contains every catalog and advice pack shipped with AIQA.
//
//go:embed catalog/*.json packs/*.json
var Files embed.FS
