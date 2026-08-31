// Package manifesthttp holds the registry's one outbound HTTP capability.
// It reads a configured manifest address; it never discovers addresses from
// returned content and never follows a redirect to another destination.
package manifesthttp

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

const maxBody = 1024 * 1024

type Client struct{ http *http.Client }

func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Deployment config names the reachable address. An ambient HTTP proxy
	// must not silently turn that into a different egress route.
	transport.Proxy = nil
	return &Client{http: &http.Client{
		Transport:     transport,
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) Fetch(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, domain.ErrDriftHTTPStatus
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, domain.ErrDriftBodyTooLarge
	}
	return body, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }
