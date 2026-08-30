// Package profiledata embeds compatibility-profile assets.
package profiledata

import "embed"

// Files contains every compatibility profile shipped with the binary.
//
//go:embed *.json
var Files embed.FS
