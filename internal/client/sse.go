package client

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// SSEClient streams /api/ai/events. The connection has no timeout: the server
// keeps it open indefinitely and sends `heartbeat` to prove liveness.
type SSEClient struct {
	client *Client
	http   *http.Client
}

func NewSSEClient(c *Client) *SSEClient {
	return &SSEClient{client: c, http: &http.Client{Timeout: 0}}
}

// Subscribe streams events until the handler returns false or ctx is cancelled.
// ready is closed once the stream is established, so callers can send the
// command that produces the events only after they can observe them.
func (s *SSEClient) Subscribe(ctx context.Context, path string, ready chan<- struct{}, handler func(SSEEvent) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.client.baseURL+path, nil)
	if err != nil {
		return cliexit.New(cliexit.CodeUsage, "cannot build SSE request: %v", err)
	}
	if s.client.token != "" {
		req.Header.Set("Authorization", BearerToken(s.client.token))
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := s.http.Do(req)
	if err != nil {
		return transportError(http.MethodGet, s.client.baseURL+path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return httpStatusError(http.MethodGet, path, resp.StatusCode, nil)
	}
	if ready != nil {
		close(ready)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)

	var current SSEEvent
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			if current.Data != "" || current.Event != "" {
				if !handler(current) {
					return nil
				}
				current = SSEEvent{}
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			current.Event = value
		case "data":
			if current.Data != "" {
				current.Data += "\n"
			}
			current.Data += value
		case "id":
			current.ID = value
		}
	}
	return scanner.Err()
}

// SubscribeWithRetry reconnects with backoff until ctx is cancelled or the
// handler stops the stream. A dropped SSE connection is expected during long
// agent runs (proxy idle timeouts, laptop sleep) and must not fail the command.
func (s *SSEClient) SubscribeWithRetry(ctx context.Context, path string, ready chan<- struct{}, handler func(SSEEvent) bool) error {
	backoff := time.Second
	const maxBackoff = 15 * time.Second
	first := true

	for {
		var readyOnce chan<- struct{}
		if first {
			readyOnce = ready
		}
		err := s.Subscribe(ctx, path, readyOnce, handler)
		first = false

		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		// Auth failures never recover by retrying.
		if cliexit.CodeOf(err) == cliexit.CodeAuth {
			return err
		}
		fmt.Fprintf(os.Stderr, "SSE stream dropped (%v); reconnecting in %s\n", err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}
