package addonbuild

// SetDetectGlobsForTest points Detect's two globs (the plain Documents path and the
// OneDrive-redirected one) at test folders and returns the restore. It is compiled only
// into this package's test binary, so the external test package (addonbuild_test) can
// use it; production code cannot see it.
func SetDetectGlobsForTest(docs, oneDrive string) (restore func()) {
	origDocs, origOneDrive := nt8DocsGlob, nt8OneDriveGlob
	nt8DocsGlob, nt8OneDriveGlob = docs, oneDrive
	return func() { nt8DocsGlob, nt8OneDriveGlob = origDocs, origOneDrive }
}
