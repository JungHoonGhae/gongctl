package portal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DefaultBaseURL is the public data portal root.
const DefaultBaseURL = "https://www.data.go.kr"

// DefaultUserAgent identifies the client honestly to the server operator.
const DefaultUserAgent = "gongctl (+https://github.com/JungHoonGhae/gongctl)"

// Client is a rate-limited HTTP client for scraping data.go.kr public pages
// (dataset search, OpenAPI detail). Authenticated actions use the CDP browser
// (browser.go), not this client.
type Client struct {
	baseURL   string
	userAgent string
	delay     time.Duration
	http      *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

// Option configures a Client.
type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithUserAgent(ua string) Option       { return func(c *Client) { c.userAgent = ua } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithDelay(d time.Duration) Option {
	return func(c *Client) {
		if d >= 0 {
			c.delay = d
		}
	}
}

// New creates a Client with sane defaults.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		userAgent: DefaultUserAgent,
		delay:     DefaultDelay,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

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

func (c *Client) getDoc(ctx context.Context, path string, query url.Values) (*goquery.Document, error) {
	c.throttle()
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/json,*/*")
	req.Header.Set("Referer", c.baseURL+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("GET %s: unexpected status %s", path, resp.Status)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}
