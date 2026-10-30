package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxDialBody is how much of a refused handshake's HTTP body the error keeps.
const maxDialBody = 200

// dialError names what the server answered when the WebSocket dial fails.
// gorilla's "bad handshake" alone hides it: on 2026-10-29 both mvp bots and
// their rollback logged only that for an hour (#524), and the HTTP status
// (503 maintenance, 403 blocked, 404 moved endpoint) is what tells a Deribit
// or network cause from a code one. The endpoint is a public URL, never a
// credential.
func dialError(endpoint string, resp *http.Response, err error) error {
	if resp == nil {
		return fmt.Errorf("websocket dial %s: %w", endpoint, err)
	}
	detail := resp.Status
	if resp.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxDialBody))
		resp.Body.Close()
		if text := strings.Join(strings.Fields(string(b)), " "); text != "" {
			detail += ": " + text
		}
	}
	return fmt.Errorf("websocket dial %s: %w (HTTP %s)", endpoint, err, detail)
}
