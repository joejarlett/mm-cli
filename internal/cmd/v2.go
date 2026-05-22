package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mm-cli/internal/apps"
	mmhttp "mm-cli/internal/http"
)

// NewV2Cmd builds the universal-verb dispatcher under any registered app
// slug. `mm v2 <app> <feature.action>` raw dispatch is also exposed.
func NewV2Cmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "v2 [app] [feature.action] [k=v...]",
		Short: "Raw dispatch to <app>/api/v2 (alias for `mm <app> <feature> <action>`)",
		Args:  cobra.MinimumNArgs(2),
		RunE:  runV2,
	}
	c.Flags().String("instance", "", "X-Hub-Instance-Id header")
	c.Flags().Bool("no-validate", false, "Skip manifest pre-validation")
	return c
}

func runV2(cmd *cobra.Command, args []string) error {
	appSlug := args[0]
	featureAction := args[1]
	kv := args[2:]
	payload := parseKV(kv)

	instance, _ := cmd.Flags().GetString("instance")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	app, err := apps.Resolve(appSlug)
	if err != nil {
		return err
	}
	client := mmhttp.New()
	res, err := client.V2(cmd.Context(), app.URL, featureAction, payload, mmhttp.V2Opts{InstanceID: instance})
	if err != nil {
		return err
	}
	if wantJSON {
		fmt.Println(string(res.Body))
		return nil
	}
	var v any
	if json.Unmarshal(res.Body, &v) == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Println(string(res.Body))
	}
	if !res.OK {
		return fmt.Errorf("HTTP %d", res.Status)
	}
	return nil
}

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
