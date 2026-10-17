package updaterwire

import (
	"bufio"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"vl/internal/safepath"
)

// privateSockDir returns <tmp>/data/updater at 0700 and the socket path
// SocketPath resolves inside it.
func privateSockDir(t *testing.T) (dataDir, sock string) {
	t.Helper()
	// short base: unix socket paths are capped at 107 bytes
	base, err := os.MkdirTemp("", "m3w")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = safepath.RemoveAllTemp(base) })
	dataDir = filepath.Join(base, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, UpdaterDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	sock, err = SocketPath(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return dataDir, sock
}

// echoListener is a bare stand-in for the worker (the real one is
// wireserver.Listen, tested in its own package): it answers every status
// frame with {"ok":true,"state":"idle"}.
func echoListener(t *testing.T, sock string, mode os.FileMode) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sock, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			go func(c *net.UnixConn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					f, err := ReadFrame(br)
					if err != nil {
						return
					}
					if _, err := DecodeRequest(f); err != nil {
						return
					}
					out, _ := EncodeResponse(Response{OK: true, State: "idle"})
					c.Write(out)
				}
			}(c)
		}
	}()
}

func dialStatus(sock string) error {
	c, err := Dial(sock)
	if err != nil {
		return err
	}
	defer c.Close()
	resp, err := c.Do(NewStatus(""))
	if err != nil {
		return err
	}
	if !resp.OK || resp.State != "idle" {
		return errors.New("unexpected response")
	}
	return nil
}

func TestDialStatusRoundTrip(t *testing.T) {
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o600)
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control: a 0600 socket we own in a 0700 dir must answer status, got %v", err)
	}
}

func TestDialRefusesALooseSocket(t *testing.T) {
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o666)
	if err := dialStatus(sock); !errors.Is(err, ErrLoosePerms) {
		t.Fatalf("a 0666 socket must be refused with ErrLoosePerms, got %v", err)
	}
	// positive control: the one defect removed
	if err := os.Chmod(sock, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control after chmod 0600: %v", err)
	}
}

func TestDialRefusesALooseDir(t *testing.T) {
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o600)
	if err := os.Chmod(filepath.Dir(sock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dialStatus(sock); !errors.Is(err, ErrLoosePerms) {
		t.Fatalf("a socket in a 0755 dir must be refused, got %v", err)
	}
	if err := os.Chmod(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control after chmod 0700: %v", err)
	}
}

func TestDialRefusesASocketNotOwnedByUs(t *testing.T) {
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o600)
	// seam: the socket FILE (only) reads as another uid's; the dir stays ours
	orig := fileOwner
	t.Cleanup(func() { fileOwner = orig })
	fileOwner = func(fi fs.FileInfo) (uint32, bool) {
		uid, ok := orig(fi)
		if fi.Mode()&fs.ModeSocket != 0 {
			return uid + 1, ok
		}
		return uid, ok
	}
	if err := dialStatus(sock); !errors.Is(err, ErrForeignOwner) {
		t.Fatalf("a socket owned by another uid must be refused with ErrForeignOwner, got %v", err)
	}
	fileOwner = orig
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control with the real owner: %v", err)
	}
}

func TestDialRefusesADirNotOwnedByUs(t *testing.T) {
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o600)
	orig := fileOwner
	t.Cleanup(func() { fileOwner = orig })
	fileOwner = func(fi fs.FileInfo) (uint32, bool) {
		uid, ok := orig(fi)
		if fi.IsDir() {
			return uid + 1, ok
		}
		return uid, ok
	}
	if err := dialStatus(sock); !errors.Is(err, ErrForeignOwner) {
		t.Fatalf("a socket inside another uid's dir must be refused, got %v", err)
	}
	fileOwner = func(fi fs.FileInfo) (uint32, bool) { return 0, false }
	if err := dialStatus(sock); !errors.Is(err, ErrForeignOwner) {
		t.Fatalf("an unreadable owner must be refused (fail closed), got %v", err)
	}
	fileOwner = orig
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control: %v", err)
	}
}

func TestDialRefusesAListenerOfAnotherUID(t *testing.T) {
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o600)
	orig := dialPeerUID
	t.Cleanup(func() { dialPeerUID = orig })
	dialPeerUID = func(*net.UnixConn) (uint32, error) { return uint32(os.Geteuid()) + 1, nil }
	if err := dialStatus(sock); !errors.Is(err, ErrPeerUID) {
		t.Fatalf("a listener whose SO_PEERCRED uid is not ours must be refused, got %v", err)
	}
	dialPeerUID = func(*net.UnixConn) (uint32, error) { return 0, errors.New("getsockopt failed") }
	if err := dialStatus(sock); !errors.Is(err, ErrPeerUID) {
		t.Fatalf("an unreadable SO_PEERCRED must be refused (fail closed), got %v", err)
	}
	dialPeerUID = orig
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control with the real SO_PEERCRED: %v", err)
	}
}

func TestDialRefusesASymlinkOrRegularFile(t *testing.T) {
	_, sock := privateSockDir(t)
	// a real, good socket elsewhere in the same private dir
	real := filepath.Join(filepath.Dir(sock), "real.sock")
	echoListener(t, real, 0o600)
	if err := os.Symlink(real, sock); err != nil {
		t.Fatal(err)
	}
	if err := dialStatus(sock); !errors.Is(err, ErrSymlink) {
		t.Fatalf("a symlink at the socket path must be refused, got %v", err)
	}
	os.Remove(sock)
	if err := os.WriteFile(sock, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := dialStatus(sock); !errors.Is(err, ErrNotSocket) {
		t.Fatalf("a regular file at the socket path must be refused, got %v", err)
	}
	os.Remove(sock)
	if err := dialStatus(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an absent socket must read as not-exist (worker not running), got %v", err)
	}
	// positive control: the real socket itself dials
	if err := dialStatus(real); err != nil {
		t.Fatalf("positive control: %v", err)
	}
}

func TestDialRefusesARelativePath(t *testing.T) {
	if _, err := Dial("data/updater/" + SocketFileName); !errors.Is(err, ErrBadPath) {
		t.Fatalf("a relative socket path must be refused, got %v", err)
	}
	_, sock := privateSockDir(t)
	echoListener(t, sock, 0o600)
	unclean := filepath.Dir(sock) + "/../" + UpdaterDirName + "/" + SocketFileName
	if _, err := Dial(unclean); !errors.Is(err, ErrBadPath) {
		t.Fatalf("a non-clean socket path must be refused, got %v", err)
	}
	if err := dialStatus(sock); err != nil {
		t.Fatalf("positive control (clean path): %v", err)
	}
}

func TestPeerCredIsReal(t *testing.T) {
	// positive control for the seam: the real SO_PEERCRED returns our own uid
	_, sock := privateSockDir(t)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan uint32, 1)
	go func() {
		c, err := ln.AcceptUnix()
		if err != nil {
			done <- ^uint32(0)
			return
		}
		defer c.Close()
		uid, err := PeerUID(c)
		if err != nil {
			done <- ^uint32(0)
			return
		}
		done <- uid
	}()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if uid, err := PeerUID(c); err != nil || uid != uint32(os.Geteuid()) {
		t.Fatalf("client-side SO_PEERCRED = %d, %v; want %d", uid, err, os.Geteuid())
	}
	if got := <-done; got != uint32(os.Geteuid()) {
		t.Fatalf("server-side SO_PEERCRED = %d, want %d", got, os.Geteuid())
	}
}
