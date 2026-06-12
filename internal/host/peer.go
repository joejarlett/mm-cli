package host

import (
	"encoding/json"
	"fmt"
	"time"
)

// peerFetch relays a telemetry read from the peer machine's own host agent:
// ssh into the peer and curl its loopback-bound API with the shared token.
// The peer reports its own host facts authoritatively; we just relay the JSON.
// Same design as infra-api.mjs peerFetch() — requires passwordless ssh to the
// peer (tailnet + key auth) and the same API token on both ends.
func peerFetch(peerHost, token, path string) (map[string]any, error) {
	remote := fmt.Sprintf("curl -s -m8 -H 'X-API-Token: %s' http://127.0.0.1:8889%s", token, path)
	out, err := run(14*time.Second, "ssh", "-o", "ConnectTimeout=5", "-o", "BatchMode=yes", peerHost, remote)
	if err != nil {
		return nil, fmt.Errorf("peer (%s) unreachable: %w", peerHost, err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, fmt.Errorf("peer (%s) returned non-JSON: %w", peerHost, err)
	}
	return data, nil
}
