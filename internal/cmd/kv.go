package cmd

import (
	"strings"
)

// parseKV turns `key=value` args into a payload map. Coerces "true"/"false"/numbers.
func parseKV(args []string) map[string]any {
	out := map[string]any{}
	for _, a := range args {
		eq := strings.IndexByte(a, '=')
		if eq <= 0 {
			continue
		}
		k := a[:eq]
		v := a[eq+1:]
		out[k] = coerce(v)
	}
	return out
}

func coerce(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	// Numbers
	if isDigits(v) {
		var n int64
		for _, c := range v {
			n = n*10 + int64(c-'0')
		}
		return n
	}
	return v
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
