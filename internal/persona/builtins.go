package persona

import "embed"

//go:embed builtins/*.yaml
var builtinsFS embed.FS
