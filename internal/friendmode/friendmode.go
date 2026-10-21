// Package friendmode holds the ONE parser for the VL_FRIEND_INSTANCE flag
// (S21 / R3). config, security, api and agent all need the answer, and
// security cannot import config (config → mcp → security import cycle), so the
// canonicalizer lives here where every package can reach it.
package friendmode

import (
	"os"
	"strings"
)

// Enabled is the ONE parser for VL_FRIEND_INSTANCE: "1" or "true" (trimmed,
// case-insensitive). Anything else — including "" — is false (owner's box).
func Enabled() bool {
	v := strings.TrimSpace(os.Getenv("VL_FRIEND_INSTANCE"))
	return v == "1" || strings.EqualFold(v, "true")
}
