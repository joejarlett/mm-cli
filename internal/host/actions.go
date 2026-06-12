package host

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ActionResult mirrors infra-api.mjs's { ok, output } / { ok, error } shape.
type ActionResult struct {
	Ok     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// nameRe restricts container names / service labels to docker's charset —
// they're passed as exec args (not a shell), but tight validation costs nothing.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ContainerAction runs docker restart/stop/start, refusing protected containers.
func ContainerAction(name, action string) ActionResult {
	if protectedContainers[name] {
		return ActionResult{Ok: false, Error: "protected container"}
	}
	switch action {
	case "restart", "stop", "start":
	default:
		return ActionResult{Ok: false, Error: "unknown action"}
	}
	out, err := run(30*time.Second, dockerBin(), action, name)
	if err != nil {
		return ActionResult{Ok: false, Error: strings.TrimSpace(fmt.Sprintf("%s: %s", err, out))}
	}
	return ActionResult{Ok: true, Output: strings.TrimSpace(out)}
}

// ServiceAction restarts/starts a tracked service, refusing protected ones.
// The actual invocation is per-platform (launchctl vs systemctl).
func ServiceAction(label, action string) ActionResult {
	if protectedServices[label] {
		return ActionResult{Ok: false, Error: "protected service"}
	}
	switch action {
	case "restart", "start":
	default:
		return ActionResult{Ok: false, Error: "unknown action"}
	}
	out, err := serviceActionPlatform(label, action)
	if err != nil {
		return ActionResult{Ok: false, Error: strings.TrimSpace(fmt.Sprintf("%s: %s", err, out))}
	}
	return ActionResult{Ok: true, Output: strings.TrimSpace(out)}
}
