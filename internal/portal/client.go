package portal

import (
	"context"
	"net/url"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"github.com/PuerkitoBio/goquery"
)

// Client scrapes data.go.kr public pages (dataset search). It owns the base URL
// and query building; every HTTP request goes through an injected fetch.Client,
// so search shares one throttle with describe and call. Authenticated actions
// use the CDP browser (browser.go), not this client.
type Client struct {
	baseURL string
	fetch   *fetch.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the data.go.kr root (useful for tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// New builds a Client over an injected fetch transport.
func New(f *fetch.Client, opts ...Option) *Client {
	c := &Client{baseURL: BaseURL, fetch: f}
	for _, o := range opts {
		o(c)
	}
	return c
}

// getDoc builds a full URL from the base + path + query and fetches it as HTML
// through the shared transport.
func (c *Client) getDoc(ctx context.Context, path string, query url.Values) (*goquery.Document, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return c.fetch.GetDoc(ctx, u)
}
