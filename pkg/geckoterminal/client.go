package geckoterminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	DefaultBaseURL        = "https://api.geckoterminal.com/api/v2"
	DefaultMinInterval    = 2 * time.Second
	DefaultRequestTimeout = 10 * time.Second
	userAgent             = "plutus/0.1 (+https://github.com/dorman99/plutus)"
)

// DefaultBackoff is the wait before each retry of a 429 or 5xx response.
var DefaultBackoff = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

// HTTPError is a non-200 response.
type HTTPError struct {
	URL      string
	Status   int
	Attempts int
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("GET %s: HTTP %d after %d attempt(s)", e.URL, e.Status, e.Attempts)
	if e.Status == http.StatusForbidden {
		msg += " (blocked by the API's bot protection; wait a few minutes and retry)"
	}
	return msg
}

// Client is safe for concurrent use. All calls through one Client share a
// single rate limiter: at least MinInterval passes between any two requests.
type Client struct {
	baseURL     string
	http        *http.Client
	minInterval time.Duration
	reqTimeout  time.Duration
	backoff     []time.Duration

	mu   sync.Mutex
	last time.Time
}

// Option configures a Client.
type Option func(*Client)

func WithBaseURL(u string) Option               { return func(c *Client) { c.baseURL = u } }
func WithHTTPClient(h *http.Client) Option      { return func(c *Client) { c.http = h } }
func WithMinInterval(d time.Duration) Option    { return func(c *Client) { c.minInterval = d } }
func WithRequestTimeout(d time.Duration) Option { return func(c *Client) { c.reqTimeout = d } }
func WithBackoff(b []time.Duration) Option      { return func(c *Client) { c.backoff = b } }

// New returns a Client with free-tier-safe defaults.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:     DefaultBaseURL,
		http:        &http.Client{},
		minInterval: DefaultMinInterval,
		reqTimeout:  DefaultRequestTimeout,
		backoff:     DefaultBackoff,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Pools fetches one page of a network pool listing, with base token,
// quote token and dex included.
func (c *Client) Pools(ctx context.Context, network string, ep Endpoint, page int) ([]Pool, error) {
	u := fmt.Sprintf("%s/networks/%s/%s?page=%d&include=%s",
		c.baseURL, url.PathEscape(network), ep, page, url.QueryEscape("base_token,quote_token,dex"))
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return DecodePools(body)
}

// get performs a GET with rate limiting and retries. It holds the limiter
// for the whole call so retries also respect the minimum interval.
func (c *Client) get(ctx context.Context, u string) (io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	attempts := 1 + len(c.backoff)
	var lastErr error
	for i := 0; i < attempts; i++ {
		wait := time.Until(c.last.Add(c.minInterval))
		if i > 0 && c.backoff[i-1] > wait {
			wait = c.backoff[i-1]
		}
		if err := sleep(ctx, wait); err != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("%v; gave up: %w", lastErr, err)
			}
			return nil, err
		}

		body, status, err := c.do(ctx, u)
		c.last = time.Now()
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, fmt.Errorf("GET %s: %w", u, ctx.Err())
			}
			lastErr = fmt.Errorf("GET %s: %w", u, err) // network error or per-request timeout: retry
		case status == http.StatusOK:
			return body, nil
		case status == http.StatusTooManyRequests || status >= 500:
			lastErr = &HTTPError{URL: u, Status: status, Attempts: i + 1}
		default:
			return nil, &HTTPError{URL: u, Status: status, Attempts: i + 1}
		}
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, u string) (io.ReadCloser, int, error) {
	rctx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u, nil)
	if err != nil {
		cancel()
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		cancel()
		return nil, resp.StatusCode, nil
	}
	return &cancelBody{ReadCloser: resp.Body, cancel: cancel}, resp.StatusCode, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// sleep waits for d, but refuses to start a wait that would end after the
// context deadline, so a retry never burns the rest of the scan budget.
func sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	if dl, ok := ctx.Deadline(); ok && time.Now().Add(d).After(dl) {
		return fmt.Errorf("next request would start after the deadline: %w", context.DeadlineExceeded)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsDeadline reports whether err was caused by running out of time.
func IsDeadline(err error) bool { return errors.Is(err, context.DeadlineExceeded) }
