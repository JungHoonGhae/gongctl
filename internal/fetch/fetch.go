// Package fetch is the one throttled HTTP transport for every data.go.kr
// request gongctl makes over plain HTTP — dataset search and OpenAPI describe
// (www.data.go.kr) and authenticated call (apis.data.go.kr). It hides the
// User-Agent, timeout, rate-limit throttle, and (for GetDoc) status-gating +
// goquery parsing behind a two-method interface, so callers keep only their
// parse/shape logic. The CDP browser session (internal/portal/browser.go) is a
// separate transport and does not go through here.
package fetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DefaultUserAgent identifies gongctl honestly to the server operator.
const DefaultUserAgent = "gongctl (+https://github.com/JungHoonGhae/gongctl)"

// DefaultDelay is the minimum spacing between requests (politeness throttle).
const DefaultDelay = 700 * time.Millisecond

// Response is a raw HTTP response surfaced to a caller. Body is the fully-read
// payload; Status is passed through untouched (non-200 is not an error — the
// caller decides what a given status means).
type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

// Client is a rate-limited, host-agnostic HTTP transport. A single Client
// shared across search/describe/call gives all of gongctl's data.go.kr traffic
// one throttle. Safe for concurrent use.
type Client struct {
	userAgent string
	delay     time.Duration
	http      *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithHTTPClient injects a custom *http.Client (timeout, transport, test hooks).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithDelay sets the minimum spacing between requests. Zero disables throttling.
func WithDelay(d time.Duration) Option {
	return func(c *Client) {
		if d >= 0 {
			c.delay = d
		}
	}
}

// New builds a Client with sane defaults.
func New(opts ...Option) *Client {
	c := &Client{
		userAgent: DefaultUserAgent,
		delay:     DefaultDelay,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ponytail: single throttle across www+apis; split per-host if throughput matters.
func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.delay > 0 && !c.lastReq.IsZero() {
		if wait := c.delay - time.Since(c.lastReq); wait > 0 {
			time.Sleep(wait)
		}
	}
	c.lastReq = time.Now()
}

// Get performs a throttled GET on a full URL and returns the raw response. Any
// HTTP status is returned as a *Response (data.go.kr answers 200 — occasionally
// non-200 — with a meaningful body); only a transport failure returns an error.
// The error, when non-nil, may include the URL — a caller passing secrets in the
// query (e.g. serviceKey) must redact it before surfacing.
func (c *Client) Get(ctx context.Context, rawURL string) (*Response, error) {
	c.throttle()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/json,application/xml,*/*")
	if ref := refererOf(rawURL); ref != "" {
		req.Header.Set("Referer", ref)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
	}, nil
}

// GetDoc performs a Get and parses the body as HTML. Unlike Get it gates on 200:
// the pages GetDoc serves (search list, OpenAPI detail) are only meaningful when
// served 200, so a non-200 is an error rather than a document to scrape.
func (c *Client) GetDoc(ctx context.Context, rawURL string) (*goquery.Document, error) {
	res, err := c.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if res.Status != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", rawURL, res.Status)
	}
	return goquery.NewDocumentFromReader(bytes.NewReader(res.Body))
}

// refererOf returns "scheme://host/" for a URL, or "" if it can't be parsed.
// data.go.kr's front end is friendlier to requests that carry a same-site Referer.
func refererOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}
