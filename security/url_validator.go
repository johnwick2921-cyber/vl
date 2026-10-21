// Package security provides security utilities for the application
package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vl/internal/friendmode"
)

// Private/Reserved IP ranges that should be blocked to prevent SSRF
var privateIPBlocks []*net.IPNet

func init() {
	// Initialize private IP blocks
	// These ranges should not be accessible via user-controlled URLs
	privateRanges := []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918 private
		"172.16.0.0/12",  // RFC1918 private
		"192.168.0.0/16", // RFC1918 private
		"169.254.0.0/16", // Link-local / Cloud metadata
		"100.64.0.0/10",  // Tailscale CGNAT (RFC 6598) — R2
		"0.0.0.0/8",      // Current network
		"224.0.0.0/4",    // Multicast
		"240.0.0.0/4",    // Reserved
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local
		"64:ff9b::/96",   // NAT64 well-known prefix — R2
		"2002::/16",      // 6to4 — R2
	}

	for _, cidr := range privateRanges {
		_, block, err := net.ParseCIDR(cidr)
		if err == nil {
			privateIPBlocks = append(privateIPBlocks, block)
		}
	}
}

// SSRFError represents a Server-Side Request Forgery attempt
type SSRFError struct {
	URL    string
	Reason string
}

func (e *SSRFError) Error() string {
	return fmt.Sprintf("SSRF blocked: %s - %s", e.URL, e.Reason)
}

// isPrivateIP checks if an IP address is in a private/reserved range
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true // Invalid IP, treat as private
	}

	// Check if it's a loopback address
	if ip.IsLoopback() {
		return true
	}

	// Check if it's a link-local address
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// Check if it's a private address
	if ip.IsPrivate() {
		return true
	}

	// Check against our explicit private ranges
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}

	return false
}

// ValidateURL checks if a URL is safe to request (not pointing to internal networks)
// Returns an error if the URL is potentially dangerous
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return &SSRFError{URL: rawURL, Reason: "empty URL"}
	}

	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return &SSRFError{URL: rawURL, Reason: "invalid URL format"}
	}

	// Only allow http and https schemes
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return &SSRFError{URL: rawURL, Reason: fmt.Sprintf("unsupported scheme: %s", scheme)}
	}

	// Extract hostname (without port)
	host := parsedURL.Hostname()
	if host == "" {
		return &SSRFError{URL: rawURL, Reason: "empty hostname"}
	}

	// Block localhost and common internal hostnames
	lowerHost := strings.ToLower(host)
	blockedHosts := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"metadata.google.internal",
		"metadata.google",
		"instance-data",
	}
	for _, blocked := range blockedHosts {
		if lowerHost == blocked {
			return &SSRFError{URL: rawURL, Reason: fmt.Sprintf("blocked hostname: %s", host)}
		}
	}

	// Resolve the hostname to IP addresses
	// This catches DNS rebinding and ensures we check the actual destination
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolver := net.Resolver{}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		// If DNS resolution fails, we still need to check if it's an IP address directly
		ip := net.ParseIP(host)
		if ip != nil {
			if isPrivateIP(ip) {
				return &SSRFError{URL: rawURL, Reason: "resolves to private IP address"}
			}
			return nil // It's a valid public IP
		}
		// DNS resolution failed, but it's not an IP - could be a typo or non-existent domain
		// Allow it and let the HTTP client handle the error
		return nil
	}

	// Check all resolved IPs
	for _, ipAddr := range ips {
		if isPrivateIP(ipAddr.IP) {
			return &SSRFError{URL: rawURL, Reason: fmt.Sprintf("resolves to private IP: %s", ipAddr.IP)}
		}
	}

	return nil
}

// ValidateHTTPSURL is ValidateURL plus a hard requirement that the scheme is
// https (U19). Friend mode must never accept a plain-http custom endpoint —
// the friend's key travels over it.
func ValidateHTTPSURL(rawURL string) error {
	if rawURL == "" {
		return &SSRFError{URL: rawURL, Reason: "empty URL"}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return &SSRFError{URL: rawURL, Reason: "invalid URL format"}
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return &SSRFError{URL: rawURL, Reason: fmt.Sprintf("scheme must be https, got %q", parsed.Scheme)}
	}
	return ValidateURL(rawURL)
}

// FriendModeEnabled reports whether this process is a friend instance for the
// redirect guard (R2) and the model-URL validator (R3). It defaults to the ONE
// parser (internal/friendmode) so tests using t.Setenv see it live; main.go may
// pin it to config.FriendInstanceEnabled for explicitness.
var FriendModeEnabled = friendmode.Enabled

// redirectValidator is the ONE redirect validator: friend mode demands https on
// every hop (the friend's key travels over it); the owner's box blocks only
// private targets via ValidateURL.
func redirectValidator(rawURL string) error {
	if FriendModeEnabled() {
		return ValidateHTTPSURL(rawURL)
	}
	return ValidateURL(rawURL)
}

// ValidateModelURL is the ONE validator for a user-supplied model custom_api_url
// (R3): friend mode demands https; the owner's box stays lenient. Empty means
// "use the provider default" and is accepted; a trailing "#" fragment the UI
// appends is trimmed.
func ValidateModelURL(rawURL string) error {
	clean := strings.TrimSuffix(strings.TrimSpace(rawURL), "#")
	if clean == "" {
		return nil
	}
	if FriendModeEnabled() {
		return ValidateHTTPSURL(clean)
	}
	return ValidateURL(clean)
}

// SafeHTTPClient returns an HTTP client with SSRF protection
// It validates URLs and blocks requests to private networks
// DialerKeepAlive is the ONE source of the TCP keepalive the safe client's
// dialer sets. CLASS 46: the class-41 boot line printed "keepalive=30s" as a
// LITERAL passed at the call site, while the wire showed 14-20 s — the line
// asserted a value nothing read. Every printer now reads THIS.
//
// Note what it does and does not promise: this is the interval Go asks the OS
// for on the socket. The interval OBSERVED on the wire is the OS's business
// and can differ; the boot line reports both, and says n/a when it cannot
// observe one, rather than implying the set value is the seen value.
const DialerKeepAlive = 30 * time.Second

func SafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: DialerKeepAlive,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
				port = "443"
			}

			// Resolve and check every IP. A private address is refused BEFORE
			// any dial.
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, fmt.Errorf("SSRF protection: failed to resolve host %s: %w", host, err)
			}

			var chosen net.IP
			for _, ip := range ips {
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("SSRF protection: blocked connection to private IP %s", ip)
				}
				if chosen == nil {
					chosen = ip
				}
			}
			if chosen == nil {
				return nil, fmt.Errorf("SSRF protection: no public IP for %s", host)
			}

			// R2: dial the VALIDATED IP, not the original hostname — dialing
			// the name again would let a DNS rebind resolve to a private IP
			// after the check above passed (TOCTOU). TLS SNI is unaffected:
			// http.Transport sets ServerName from the request URL, not the dial
			// address.
			return dialer.DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
		},
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}

			// R2: friend mode keeps redirects https-only; the owner's box stays
			// lenient (ValidateURL blocks private targets, not plain http).
			if err := redirectValidator(req.URL.String()); err != nil {
				return fmt.Errorf("SSRF protection: redirect blocked - %w", err)
			}

			return nil
		},
	}
}

// SafeGet performs a GET request with SSRF protection
// It validates the URL before making the request and uses a safe HTTP client
func SafeGet(rawURL string, timeout time.Duration) (*http.Response, error) {
	// First validate the URL
	if err := ValidateURL(rawURL); err != nil {
		return nil, err
	}

	// Use the safe HTTP client
	client := SafeHTTPClient(timeout)
	return client.Get(rawURL)
}
