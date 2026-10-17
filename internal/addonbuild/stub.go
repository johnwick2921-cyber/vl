package addonbuild

import "errors"

// errNotImplemented is the placeholder every stub returns until the real
// implementation lands. DS-104 codes against these signatures.
var errNotImplemented = errors.New("addonbuild: not implemented")
