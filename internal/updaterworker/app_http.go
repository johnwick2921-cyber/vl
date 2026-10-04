package updaterworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// CutoverTokenEnv is the environment variable the worker reads the app's
// machine token from (a gate-jwt the operator minted — the worker NEVER mints
// and never reads JWT_SECRET). The value is held in memory only: never
// logged, never in the job file, never in a receipt's evidence
// (TestNoTokenEverReachesTheJobFileOrEvidence).
const CutoverTokenEnv = "VL_CUTOVER_TOKEN"

// ErrNoToken: the worker was started without the cutover token.
var ErrNoToken = errors.New("updaterworker: " + CutoverTokenEnv + " is not set (the worker never mints a token)")

const (
	appTimeout  = 5 * time.Second
	maxAppBytes = 1 << 20
)

// HTTPApp is the production AppReader: GETs on loopback only.
type HTTPApp struct {
	base   string // http://127.0.0.1:<port>
	token  string // never printed: String/GoString/Format redact it
	client *http.Client
}

// NewHTTPApp reads the token from the environment. base must be a loopback
// http URL; anything else is refused (the worker never talks off-box).
func NewHTTPApp(base string) (*HTTPApp, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("updaterworker: app base %q is not an http origin", base)
	}
	if h := u.Hostname(); h != "127.0.0.1" && h != "::1" && h != "localhost" {
		return nil, fmt.Errorf("updaterworker: app base must be loopback, got host %q", h)
	}
	tok := strings.TrimSpace(os.Getenv(CutoverTokenEnv)) // R5: VL_ only
	if tok == "" {
		return nil, ErrNoToken
	}
	return &HTTPApp{
		base:  strings.TrimRight(base, "/"),
		token: tok,
		client: &http.Client{Timeout: appTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // a redirect would carry the Bearer elsewhere
		}},
	}, nil
}

// String never reveals the token.
func (a *HTTPApp) String() string { return "HTTPApp{" + a.base + ", token=<redacted>}" }

// GoString never reveals the token.
func (a *HTTPApp) GoString() string { return a.String() }

func (a *HTTPApp) get(ctx context.Context, path string, auth bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, appTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		// url.Error quotes the URL (no token: the token rides a header)
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAppBytes+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: read: %w", path, err)
	}
	if len(b) > maxAppBytes {
		return nil, fmt.Errorf("GET %s: body larger than %d bytes", path, maxAppBytes)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("GET %s: %w", path, ErrUnauthorized)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	return b, nil
}

// requireKeys refuses a JSON object missing any key the worker reads — a
// field that was never computed must not read as false/zero. A key in
// nullable may be null (the app's "unknown": job_id with no hold, addon_ack
// with no ack); every other listed key must be present and non-null.
func requireKeys(b []byte, nullable map[string]bool, keys ...string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("not a JSON object")
	}
	for _, k := range keys {
		v, ok := raw[k]
		if !ok {
			return fmt.Errorf("%q missing", k)
		}
		if !nullable[k] && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("%q is null", k)
		}
	}
	return nil
}

// Health is GET /api/health (no auth) → its "revision".
func (a *HTTPApp) Health(ctx context.Context) (string, error) {
	b, err := a.get(ctx, "/api/health", false)
	if err != nil {
		return "", err
	}
	var v struct {
		Revision *string `json:"revision"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", fmt.Errorf("health: %w", err)
	}
	if v.Revision == nil || *v.Revision == "" {
		return "", errors.New("health names no revision")
	}
	return *v.Revision, nil
}

// Maintenance is GET /api/maintenance (Bearer).
func (a *HTTPApp) Maintenance(ctx context.Context) (MaintenanceView, error) {
	b, err := a.get(ctx, "/api/maintenance", true)
	if err != nil {
		return MaintenanceView{}, err
	}
	return decodeMaintenance(b)
}

func decodeMaintenance(b []byte) (MaintenanceView, error) {
	if err := requireKeys(b, map[string]bool{"job_id": true, "addon_ack": true}, "held", "state", "job_id", "in_flight_sends", "drained", "addon_ack"); err != nil {
		return MaintenanceView{}, fmt.Errorf("maintenance view: %w", err)
	}
	var v MaintenanceView
	if err := json.Unmarshal(b, &v); err != nil {
		return MaintenanceView{}, fmt.Errorf("maintenance view: %w", err)
	}
	if v.AddonAck != nil && v.AddonAck.Received == "" {
		return MaintenanceView{}, errors.New("maintenance view: addon_ack without a received time")
	}
	return v, nil
}

// InstallationGate is GET /api/installation-gate (Bearer).
func (a *HTTPApp) InstallationGate(ctx context.Context) (GateView, error) {
	b, err := a.get(ctx, "/api/installation-gate", true)
	if err != nil {
		return GateView{}, err
	}
	return decodeGate(b)
}

func decodeGate(b []byte) (GateView, error) {
	if err := requireKeys(b, map[string]bool{"nt8_absent": true}, "ready", "job_id", "legs"); err != nil {
		return GateView{}, fmt.Errorf("installation gate: %w", err)
	}
	var v GateView
	if err := json.Unmarshal(b, &v); err != nil {
		return GateView{}, fmt.Errorf("installation gate: %w", err)
	}
	return v, nil
}

// Index is GET / (the served UI shell).
func (a *HTTPApp) Index(ctx context.Context) ([]byte, error) {
	return a.get(ctx, "/", false)
}
