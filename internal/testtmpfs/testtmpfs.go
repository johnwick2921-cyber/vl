// Package testtmpfs puts a test binary's temporary files on tmpfs (/dev/shm).
//
// Why: most of the repo's test time is not CPU. Every test builds its own
// SQLite file under t.TempDir() and the migrations fsync statement by
// statement; on this box's virtual ext4 disk that is a fixed ~1.2 s per test
// (api, store, trader, ninjatrader, agent, updaterbootstrap, updaterworker),
// against ~0.02 s on tmpfs. The same assertions run; only where the bytes
// live changes (tmpfs keeps rename, flock and permissions).
//
// A package installs it in its TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testtmpfs.Run(m.Run)) }
//
// Guards (it never fails a run): an explicit TMPDIR, VL_TEST_NO_TMPFS=1, a
// missing or unwritable /dev/shm, under 1 GiB free, or a non-Linux OS keep the
// default TMPDIR (one stderr line says why). The private dir is vlt-<pid>,
// removed on exit; dirs of killed runs (dead pid) are swept at the next start.
package testtmpfs

// Run sets TMPDIR to a private tmpfs dir, calls fn (the package's m.Run or a
// wrapper around it) and returns its exit code, removing the dir afterwards.
func Run(fn func() int) int { return run(fn) }
