package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Some ISP routes to individual CloudFront edges reset most connections
// while other edges for the same hostname work perfectly — and which edges
// you get depends on the resolver and rotates over time. Defenses:
//
//   - edgeStats.resolve merges the system resolver's answer with public
//     resolvers' (CloudFront answers differ per resolver), so a healthy edge
//     is almost always among the candidates. Known ISP block-page addresses
//     are dropped.
//   - edgeStats.dial tries addresses best-first by smoothed failure rate and
//     demotes any address that resets on us. Stats can persist to disk so a
//     one-shot CLI run starts on known-good edges.
//   - Client.fetch retries failed requests with backoff. Every TMDb call is a
//     read-only GET, so retrying is always safe.

const (
	maxAttempts = 5
	dialTimeout = 5 * time.Second
	dnsTimeout  = 2 * time.Second
	resolveTTL  = 10 * time.Minute
	edgeMaxAge  = 7 * 24 * time.Hour // persisted stats older than this are ignored
)

// baseBackoff is the first retry delay; it doubles per attempt (plus up to
// 50% jitter). A var so tests can shrink it.
var baseBackoff = 300 * time.Millisecond

// publicResolvers supplement the system resolver. Only the TMDb API
// hostname is ever looked up through them.
var publicResolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

// edgeRecord is one address's track record.
type edgeRecord struct {
	Fails int       `json:"fails"`
	OKs   int       `json:"oks"`
	Seen  time.Time `json:"seen"`
}

// score is a smoothed failure rate: untried = 0.5, lower is better.
func (r edgeRecord) score() float64 {
	return float64(r.Fails+1) / float64(r.Fails+r.OKs+2)
}

// edgeStats tracks per-address outcomes and resolved addresses.
type edgeStats struct {
	mu    sync.Mutex
	edges map[string]edgeRecord
	path  string // "" = in-memory only
	dirty bool

	resolved   map[string][]string // host → merged addresses
	resolvedAt map[string]time.Time
	lookup     func(ctx context.Context, host string) []string // swappable in tests
}

func newEdgeStats() *edgeStats {
	e := &edgeStats{
		edges:      map[string]edgeRecord{},
		resolved:   map[string][]string{},
		resolvedAt: map[string]time.Time{},
	}
	e.lookup = mergedLookup
	return e
}

func (e *edgeStats) record(ip string, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.edges[ip]
	if ok {
		r.OKs++
	} else {
		r.Fails++
	}
	r.Seen = time.Now()
	e.edges[ip] = r
	e.dirty = true
}

func (e *edgeStats) fail(ip string) { e.record(ip, false) }
func (e *edgeStats) ok(ip string)   { e.record(ip, true) }

// order returns ips best-first by smoothed failure rate. Ties are shuffled
// so load spreads across equally good edges.
func (e *edgeStats) order(ips []string) []string {
	out := append([]string(nil), ips...)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	e.mu.Lock()
	defer e.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		return e.edges[out[i]].score() < e.edges[out[j]].score()
	})
	return out
}

// resolve returns the merged candidate addresses for host, cached briefly.
func (e *edgeStats) resolve(ctx context.Context, host string) ([]string, error) {
	if net.ParseIP(host) != nil {
		return []string{host}, nil
	}
	e.mu.Lock()
	if ips, ok := e.resolved[host]; ok && time.Since(e.resolvedAt[host]) < resolveTTL {
		e.mu.Unlock()
		return ips, nil
	}
	e.mu.Unlock()

	ips := e.lookup(ctx, host)
	if len(ips) == 0 {
		return nil, fmt.Errorf("could not resolve %s", host)
	}
	e.mu.Lock()
	e.resolved[host], e.resolvedAt[host] = ips, time.Now()
	e.mu.Unlock()
	return ips, nil
}

// mergedLookup queries the system resolver and the public resolvers in
// parallel and returns the deduplicated union, minus ISP block pages
// (unless nothing else came back).
func mergedLookup(ctx context.Context, host string) []string {
	type answer struct{ ips []string }
	ch := make(chan answer, 1+len(publicResolvers))
	query := func(r *net.Resolver) {
		cctx, cancel := context.WithTimeout(ctx, dnsTimeout)
		defer cancel()
		ips, _ := r.LookupHost(cctx, host)
		ch <- answer{ips}
	}
	go query(net.DefaultResolver)
	for _, addr := range publicResolvers {
		addr := addr
		go query(&net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}})
	}
	seen := map[string]bool{}
	var good, blocked []string
	for i := 0; i < 1+len(publicResolvers); i++ {
		for _, ip := range (<-ch).ips {
			if seen[ip] {
				continue
			}
			seen[ip] = true
			if IsBlockPage(ip) {
				blocked = append(blocked, ip)
			} else {
				good = append(good, ip)
			}
		}
	}
	if len(good) == 0 {
		return blocked
	}
	return good
}

// IsBlockPage reports whether ip looks like an Indian ISP DNS-hijack block
// page rather than a real TMDb (CloudFront) address.
func IsBlockPage(ip string) bool {
	return strings.HasPrefix(ip, "49.44.") || strings.HasPrefix(ip, "49.45.")
}

// dial connects to the best-ranked reachable address for addr's host.
func (e *edgeStats) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := e.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: dialTimeout}
	var lastErr error
	for _, ip := range e.order(ips) {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if err != nil {
			e.fail(ip)
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		return &trackedConn{Conn: c, ip: ip, stats: e}, nil
	}
	return nil, lastErr
}

// load reads persisted stats from path and remembers it for save. Missing,
// corrupt, or stale data is simply ignored — this is an optimization.
func (e *edgeStats) load(path string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.path = path
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var edges map[string]edgeRecord
	if json.Unmarshal(raw, &edges) != nil {
		return
	}
	for ip, r := range edges {
		if time.Since(r.Seen) < edgeMaxAge {
			e.edges[ip] = r
		}
	}
}

// save writes stats back atomically if anything changed since the last save.
func (e *edgeStats) save() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.path == "" || !e.dirty {
		return
	}
	raw, err := json.MarshalIndent(e.edges, "", "  ")
	if err != nil {
		return
	}
	tmp := fmt.Sprintf("%s.%d.tmp", e.path, os.Getpid())
	if os.WriteFile(tmp, raw, 0o644) == nil && os.Rename(tmp, e.path) == nil {
		e.dirty = false
	}
}

// trackedConn reports a connection reset against its edge address, and its
// first successful read as a success.
type trackedConn struct {
	net.Conn
	ip     string
	stats  *edgeStats
	once   sync.Once
	failed bool
}

func (c *trackedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.once.Do(func() { c.stats.ok(c.ip) })
	}
	if err != nil && isReset(err) && !c.failed {
		c.failed = true
		c.stats.fail(c.ip)
	}
	return n, err
}

func (c *trackedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil && isReset(err) && !c.failed {
		c.failed = true
		c.stats.fail(c.ip)
	}
	return n, err
}

func isReset(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}

func newHTTPClient(stats *edgeStats) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = stats.dial
	return &http.Client{Timeout: 15 * time.Second, Transport: tr}
}

// PersistEdges loads per-edge reliability stats from path and keeps them
// saved there, so later runs start on edges known to work from this network.
func (c *Client) PersistEdges(path string) {
	c.edges.load(path)
}

// fetch GETs u, retrying transport errors, truncated bodies, and
// 429/5xx responses with exponential backoff. It returns the final status
// and the fully read body. path is only used in (key-free) error messages.
func (c *Client) fetch(path, u string) (int, []byte, error) {
	defer c.edges.save()
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := baseBackoff << (attempt - 1)
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			time.Sleep(backoff + time.Duration(rand.Int63n(int64(backoff/2)+1)))
		}
		resp, err := c.http.Get(u)
		if err != nil {
			lastErr = redact(path, err)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("tmdb %s: reading response: %w", path, err)
			continue
		}
		if retryableStatus(resp.StatusCode) {
			lastErr = fmt.Errorf("tmdb %s: status %d", path, resp.StatusCode)
			continue
		}
		return resp.StatusCode, body, nil
	}
	return 0, nil, fmt.Errorf("%w (after %d attempts)", lastErr, maxAttempts)
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}
