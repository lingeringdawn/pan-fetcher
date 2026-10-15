package jackett

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is the port Jackett listens on unless configured otherwise.
const DefaultPort = 9117

// Discovery describes the outcome of probing the local machine for a Jackett
// instance. Every field is best-effort: a partially successful discovery (for
// example, config found but the service not running) still returns what it
// learned so the UI can pre-fill the form and prompt the user to start Jackett.
type Discovery struct {
	// Found reports whether any Jackett evidence was located (config file
	// and/or a listening port).
	Found bool
	// Running reports whether a live Jackett HTTP endpoint answered a probe.
	Running bool
	// URL is the base URL to use for API calls, e.g. "http://127.0.0.1:9117".
	URL string
	// APIKey is read from ServerConfig.json when available.
	APIKey string
	// AdminPassword is read from ServerConfig.json when available. Jackett
	// stores this in plain text, which is also what its admin API expects.
	AdminPassword string
	// Port is the effective port Jackett is configured to listen on.
	Port int
	// ConfigPath is the ServerConfig.json that was read, empty if none.
	ConfigPath string
	// Source explains where the values came from, for display in the UI.
	Source string
}

// serverConfig mirrors the subset of Jackett's ServerConfig.json that matters
// here. Port is a pointer because Jackett writes null when the user has never
// customised it.
type serverConfig struct {
	Port             *int   `json:"Port"`
	LocalBindAddress string `json:"LocalBindAddress"`
	APIKey           string `json:"APIKey"`
	AdminPassword    string `json:"AdminPassword"`
}

// Discover probes the local machine for a Jackett installation.
//
// Order of precedence:
//  1. JACKETT_URL / JACKETT_APIKEY environment variables (explicit override).
//  2. ServerConfig.json from the platform's standard data folders.
//  3. A live probe of the usual ports.
//
// It never returns an error: a negative result is reported through Discovery
// so the caller can present an install prompt instead of failing.
func Discover() Discovery {
	var d Discovery

	// 1. Environment overrides win, so operators can pin a remote instance.
	envURL := strings.TrimSpace(os.Getenv("JACKETT_URL"))
	envKey := strings.TrimSpace(os.Getenv("JACKETT_APIKEY"))

	// 2. Read the on-disk config, which yields both port and API key.
	var cfg *serverConfig
	if path, c := readServerConfig(); c != nil {
		cfg = c
		d.ConfigPath = path
		d.APIKey = c.APIKey
		d.AdminPassword = c.AdminPassword
		if c.Port != nil && *c.Port > 0 {
			d.Port = *c.Port
		}
		if c.LocalBindAddress != "" {
			d.URL = buildBaseURL(c.LocalBindAddress, d.Port)
		}
	}

	// 3. Probe candidate ports to see whether Jackett is actually running.
	ports := candidatePorts(d.Port, cfg)
	for _, p := range ports {
		base := buildBaseURL("127.0.0.1", p)
		if !probeJackett(base) {
			continue
		}
		d.Found = true
		d.Running = true
		d.Port = p
		d.URL = base
		d.Source = "probe"
		break
	}

	// A config file alone still counts as "found" so the UI can offer to start it.
	if !d.Found && cfg != nil {
		d.Found = true
		d.Source = "config"
		if d.URL == "" {
			if d.Port == 0 {
				d.Port = DefaultPort
			}
			d.URL = buildBaseURL("127.0.0.1", d.Port)
		}
	}

	// Environment overrides are applied last so they always take effect.
	if envURL != "" {
		d.URL = strings.TrimRight(envURL, "/")
		d.Found = true
		d.Source = "env"
		if d.Port == 0 {
			d.Port = portFromURL(d.URL)
		}
	}
	if envKey != "" {
		d.APIKey = envKey
	}
	if d.URL != "" {
		if probeJackett(d.URL) {
			d.Running = true
		}
	}
	return d
}

// candidatePorts returns the ports worth probing, de-duplicated and ordered by
// likelihood: the configured port first, then the default.
func candidatePorts(configured int, cfg *serverConfig) []int {
	var ports []int
	seen := map[int]bool{}
	add := func(p int) {
		if p > 0 && p <= 65535 && !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	if cfg != nil {
		add(configured)
	}
	if env := strings.TrimSpace(os.Getenv("JACKETT_PORT")); env != "" {
		if p, err := strconv.Atoi(env); err == nil {
			add(p)
		}
	}
	add(DefaultPort)
	return ports
}

// buildBaseURL assembles an http base URL, normalising wildcard bind addresses
// (which cannot be dialled) to the loopback address.
func buildBaseURL(host string, port int) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "*", "+", "::", "[::]":
		host = "127.0.0.1"
	}
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if port <= 0 {
		port = DefaultPort
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func portFromURL(raw string) int {
	if u, err := url.Parse(raw); err == nil {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			return p
		}
	}
	return 0
}

// configCandidates lists ServerConfig.json locations on Linux, covering native
// installs, the official Docker image, container mounts and portable installs.
//
// Jackett resolves this file from the user's config directory, not from the
// directory the binary lives in, so an install managed by pan-fetcher still
// reads and writes the location listed first here.
//
// The home directory is resolved from several sources, not just $HOME. A
// systemd service does not set HOME unless the unit asks for it, so relying on
// os.UserHomeDir alone made discovery fail on exactly the deployment this
// project targets: Jackett's config was present and its API key valid, but the
// panel reported it as unconfigured because the file could not be located.
func configCandidates() []string {
	var paths []string

	// Native installs: ~/.config/Jackett, or $XDG_CONFIG_HOME when set.
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "Jackett", "ServerConfig.json"))
	}
	for _, home := range homeDirs() {
		paths = append(paths, filepath.Join(home, ".config", "Jackett", "ServerConfig.json"))
	}

	// Official Docker image and common container mounts.
	paths = append(paths,
		"/config/Jackett/ServerConfig.json",
		"/app/Jackett/ServerConfig.json",
	)

	// Portable installs keep the data folder next to the binary or the cwd.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, "Jackett", "ServerConfig.json"),
			filepath.Join(dir, "..", "Jackett", "ServerConfig.json"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(wd, "Jackett", "ServerConfig.json"),
			filepath.Join(wd, "..", "Jackett", "ServerConfig.json"),
		)
	}

	// De-duplicate while preserving order.
	seen := map[string]bool{}
	out := paths[:0]
	for _, p := range paths {
		clean := filepath.Clean(p)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

// homeDirs returns every plausible home directory for the current user, most
// likely first.
//
// $HOME is authoritative when set. The fallbacks cover a systemd service, which
// starts with an empty environment: the passwd entry of the running user is the
// correct answer there, and the conventional root home is a last resort for the
// case where the passwd lookup itself fails (a static binary, a minimal image).
//
// Only directories that exist are returned, so a wrong guess cannot shadow the
// real one during the ordered search in readServerConfig.
func homeDirs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		if st, err := os.Stat(clean); err != nil || !st.IsDir() {
			return
		}
		seen[clean] = true
		out = append(out, clean)
	}

	add(os.Getenv("HOME"))

	// The passwd entry is authoritative under a service manager.
	if u, err := user.Current(); err == nil {
		add(u.HomeDir)
	}
	// Last resort: the conventional root home. pan-fetcher runs as root in the
	// documented systemd deployment, and Jackett (started by us with HOME set)
	// writes its config there.
	if os.Geteuid() == 0 {
		add("/root")
	}
	return out
}

// readServerConfig returns the first readable ServerConfig.json and its parsed
// contents.
func readServerConfig() (string, *serverConfig) {
	for _, path := range configCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cfg, err := parseServerConfig(data)
		if err != nil {
			continue
		}
		return path, cfg
	}
	return "", nil
}

// parseServerConfig decodes ServerConfig.json, tolerating a UTF-8 BOM and
// trailing whitespace.
func parseServerConfig(data []byte) (*serverConfig, error) {
	data = trimBOM(data)
	var cfg serverConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func trimBOM(b []byte) []byte {
	for _, bom := range [][]byte{
		{0xEF, 0xBB, 0xBF}, // UTF-8
		{0xFF, 0xFE},       // UTF-16LE
		{0xFE, 0xFF},       // UTF-16BE
	} {
		if len(b) >= len(bom) && string(b[:len(bom)]) == string(bom) {
			return b[len(bom):]
		}
	}
	return b
}

// probeJackett reports whether a Jackett instance answers at base.
//
// The UI login page is used rather than the Torznab API because it needs no API
// key, so detection still succeeds before credentials are known.
func probeJackett(base string) bool {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return false
	}

	// Fast pre-check: if nothing is listening, skip the HTTP round trip.
	if u, err := url.Parse(base); err == nil {
		host := u.Hostname()
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 700*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
	}

	// Jackett fingerprints itself in headers and redirect targets rather than in
	// page text: a bare GET /UI/Login answers 302 with Set-Cookie TestCookie=1,
	// and GET / redirects to /UI/Dashboard. Both markers survive even though the
	// body is empty, which is why matching on body text alone is unreliable —
	// an earlier version of this probe did exactly that and never detected a
	// perfectly healthy instance.
	client := &http.Client{
		Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(base + "/UI/Login")
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()
	text := strings.ToLower(string(body))
	location := strings.ToLower(resp.Header.Get("Location"))

	if strings.Contains(text, "jackett") ||
		strings.Contains(location, "/ui/") ||
		hasJackettCookie(resp.Cookies()) {
		return true
	}

	// A plain root redirect to /UI/Dashboard is the other Jackett marker.
	if resp.StatusCode == http.StatusMovedPermanently {
		if r2, err := client.Get(base + "/"); err == nil {
			loc := strings.ToLower(r2.Header.Get("Location"))
			r2.Body.Close()
			if strings.Contains(loc, "/ui/") {
				return true
			}
		}
	}
	return false
}

// hasJackettCookie reports whether the response set Jackett's TestCookie probe.
func hasJackettCookie(cookies []*http.Cookie) bool {
	for _, c := range cookies {
		if strings.EqualFold(c.Name, "TestCookie") {
			return true
		}
	}
	return false
}

// HealthCheck verifies that the given URL answers like a Jackett instance.
// Unlike Test it does not require an API key, so it can validate a URL before
// the user supplies credentials.
func HealthCheck(baseURL string) error {
	if strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("URL 为空")
	}
	if !probeJackett(baseURL) {
		return fmt.Errorf("未在该地址检测到 Jackett: %s", baseURL)
	}
	return nil
}
