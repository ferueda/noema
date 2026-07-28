package contracts

import "embed"

// Files contains the language-neutral contracts used by Go and the standalone
// agent package. Keeping one checked-in copy prevents the runtimes from
// silently drifting.
//
//go:embed agent-execution/v1/*.schema.json agents/content-scout/v1/*.schema.json
var Files embed.FS
