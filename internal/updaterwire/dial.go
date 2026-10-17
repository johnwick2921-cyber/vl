package updaterwire

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrPeerUID means the process behind the socket is not our uid (or its
// credentials could not be read).
var ErrPeerUID = errors.New("updaterwire: peer is not this uid")

// Seams: the effective uid and the kernel peer-credential lookup.
var (
	dialGeteuid = os.Geteuid
	dialPeerUID = PeerUID
)

const (
	dialTimeout    = 2 * time.Second
	requestTimeout = 10 * time.Second
)

// Client is the app's end of one connection to the worker.
type Client struct {
	mu   sync.Mutex
	conn *net.UnixConn
	br   *bufio.Reader
}

// Dial connects to the worker socket at path (from SocketPath) with the
// default dialTimeout. See DialWithTimeout.
func Dial(path string) (*Client, error) {
	return DialWithTimeout(path, dialTimeout)
}

// DialWithTimeout is Dial with the caller's dial bound. The safety checks are
// identical and happen before any connection: it refuses a path that is
// relative or not in clean form, a dir that is not a private 0700 dir we own,
// and a socket that is a symlink, not a socket, not ours or looser than 0600;
// after connecting, a listener whose SO_PEERCRED uid is not ours. An absent
// socket (worker not running) wraps fs.ErrNotExist.
func DialWithTimeout(path string, timeout time.Duration) (*Client, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrBadPath
	}
	euid := dialGeteuid()
	if err := CheckPrivateDir(filepath.Dir(path), euid); err != nil {
		return nil, err
	}
	if err := CheckSocketFile(path, euid); err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: timeout}
	c, err := d.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("updaterwire: dial: %w", err)
	}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		c.Close()
		return nil, ErrNotSocket
	}
	uid, err := dialPeerUID(uc)
	if err != nil || euid < 0 || uid != uint32(euid) {
		uc.Close()
		return nil, ErrPeerUID
	}
	return &Client{conn: uc, br: bufio.NewReader(uc)}, nil
}

// DialWorker is the app's one call: it resolves the socket for the bot's data
// dir (trader.MaintenanceDataDir() in the app) through SocketPath and Dials
// it. An empty or relative dataDir is ErrBadDataDir — never a cwd-relative
// socket.
func DialWorker(dataDir string) (*Client, error) {
	path, err := SocketPath(dataDir)
	if err != nil {
		return nil, err
	}
	return Dial(path)
}

// DialWorkerBounded is DialWorker with the caller's dial bound — the status
// route's request-time probe (a measured worker_listening, never inferred)
// uses 250 ms so a status call cannot stall on a half-dead socket.
func DialWorkerBounded(dataDir string, timeout time.Duration) (*Client, error) {
	path, err := SocketPath(dataDir)
	if err != nil {
		return nil, err
	}
	return DialWithTimeout(path, timeout)
}

// Do sends one request and reads one response with the default request
// deadline (10 s). Every verb except check answers in milliseconds; check
// can take minutes (the worker downloads + verifies) — see DoWithTimeout.
func (c *Client) Do(req Request) (Response, error) {
	return c.DoWithTimeout(req, requestTimeout)
}

// DoWithTimeout is Do with the caller's deadline. Only the app's check relay
// may lengthen it (bounded by updatersource.DefaultTimeout + margin); every
// other caller keeps the default. The request is validated before it is
// written (an invalid request never reaches the wire); the response is
// decoded strictly.
func (c *Client) DoWithTimeout(req Request, timeout time.Duration) (Response, error) {
	frame, err := EncodeRequest(req)
	if err != nil {
		return Response{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return Response{}, err
	}
	if _, err := c.conn.Write(frame); err != nil {
		return Response{}, err
	}
	out, err := ReadFrame(c.br)
	if err != nil {
		return Response{}, err
	}
	return DecodeResponse(out)
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// ResumeWorker (W-ADDON-ONE-CLICK, owner order 2026-10-17) sends
// resume{job_id} to the worker over its unix socket and accepts OK — the same
// verb the attended CLI sent, now reachable from the page's Retry button
// (api/handler_updates.go:socketResumer). It is the ONE non-CLI sender, and
// it lives here so the app never names a resume builder itself — the resume
// census stays tight (resumeWireDecls pins ResumeWorker; any other caller of
// NewResume outside this package and cmd/vl-updater is still an offender).
// On !OK the worker's own validated text is returned, never anything more.
func ResumeWorker(dataDir, jobID string) error {
	c, err := DialWorker(dataDir)
	if err != nil {
		return fmt.Errorf("updater worker unreachable (job %s): %w", jobID, err)
	}
	defer c.Close()
	resp, err := c.Do(NewResume(jobID))
	if err != nil {
		return fmt.Errorf("updater worker resume (job %s): %w", jobID, err)
	}
	if !resp.OK {
		return fmt.Errorf("updater worker refused resume for job %s: %s", jobID, resp.Error)
	}
	return nil
}
