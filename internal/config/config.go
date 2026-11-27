// Package config builds the core configuration and drivers from the environment.
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/charterhq/charter/drivers/alertmanager"
	"github.com/charterhq/charter/drivers/bifrost"
	"github.com/charterhq/charter/drivers/clickhouse"
	"github.com/charterhq/charter/drivers/mattermost"
	"github.com/charterhq/charter/drivers/mcp"
	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/driver"
)

// Env reads a variable; values may also come from a file named by NAME_FILE (secrets on disk).
func Env(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if f := os.Getenv(name + "_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err == nil {
			return string(trimNewline(b))
		}
	}
	return ""
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// Tokens reads API tokens from CHARTER_API_TOKENS: "name:token" pairs separated by commas or newlines.
func Tokens() (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.FieldsFunc(Env("CHARTER_API_TOKENS"), func(r rune) bool { return r == ',' || r == '\n' }) {
		name, tok, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok || name == "" || len(tok) < 16 {
			return nil, fmt.Errorf("CHARTER_API_TOKENS: each entry is name:token, token at least 16 characters")
		}
		// two entries glued by a lost separator read as one token with a colon inside: fail at start, not with 401 later
		if strings.ContainsAny(tok, ": \t\r") {
			return nil, fmt.Errorf("CHARTER_API_TOKENS: token of %q contains a colon or whitespace, entries must be separated by commas or newlines", name)
		}
		if _, dup := out[tok]; dup {
			return nil, fmt.Errorf("CHARTER_API_TOKENS: token of %q is used twice", name)
		}
		out[tok] = name
	}
	return out, nil
}

// Publishers reads CHARTER_PUBLISHERS: API caller names, separated by commas, allowed to publish packages.
func Publishers() map[string]bool {
	out := map[string]bool{}
	for _, n := range strings.Split(Env("CHARTER_PUBLISHERS"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = true
		}
	}
	return out
}

// MCPKeys reads gateway keys of agents from CHARTER_MCP_KEYS: "agent:key" pairs separated by commas
// or newlines; agent "*" is the key for agents without their own.
func MCPKeys() (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.FieldsFunc(Env("CHARTER_MCP_KEYS"), func(r rune) bool { return r == ',' || r == '\n' }) {
		agent, key, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok || agent == "" || key == "" || strings.ContainsAny(key, ": \t\r") {
			return nil, fmt.Errorf("CHARTER_MCP_KEYS: each entry is agent:key, entries separated by commas or newlines")
		}
		out[agent] = key
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CHARTER_MCP_KEYS is required with CHARTER_MCP_URL")
	}
	return out, nil
}

// MMBots reads agent bot tokens from CHARTER_MM_BOTS: "account:token" pairs separated by commas or newlines.
func MMBots() (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.FieldsFunc(Env("CHARTER_MM_BOTS"), func(r rune) bool { return r == ',' || r == '\n' }) {
		acc, tok, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok || acc == "" || tok == "" || strings.ContainsAny(tok, ": \t\r") {
			return nil, fmt.Errorf("CHARTER_MM_BOTS: each entry is account:token, entries separated by commas or newlines")
		}
		out[acc] = tok
	}
	return out, nil
}

// modelTools hands the model driver a real tools driver only: with the stub, steps with tools fail loudly.
func modelTools(t driver.Tools) driver.Tools {
	if _, isStub := t.(*stub.Tools); isStub {
		return nil
	}
	return t
}

// Drivers builds the driver set. A driver whose settings are missing falls back to a stub, with a warning.
func Drivers(log *slog.Logger) (driver.Set, *clickhouse.Journal, error) {
	var set driver.Set
	var journal *clickhouse.Journal

	if url := Env("CHARTER_MCP_URL"); url != "" {
		keys, err := MCPKeys()
		if err != nil {
			return set, nil, err
		}
		set.Tools = mcp.New(mcp.Config{URL: url, Keys: keys, Sep: Env("CHARTER_MCP_TOOL_SEPARATOR")})
	} else {
		log.Warn("tools driver is a stub: CHARTER_MCP_URL is not set, steps with tools will fail")
		set.Tools = &stub.Tools{}
	}

	if url, key := Env("CHARTER_BIFROST_URL"), Env("CHARTER_BIFROST_KEY"); url != "" && key != "" {
		var classes map[string]string
		if err := json.Unmarshal([]byte(Env("CHARTER_MODEL_CLASSES")), &classes); err != nil {
			return set, nil, fmt.Errorf("CHARTER_MODEL_CLASSES must be a JSON map of class to gateway model: %w", err)
		}
		var params map[string]map[string]any
		if p := Env("CHARTER_MODEL_PARAMS"); p != "" {
			if err := json.Unmarshal([]byte(p), &params); err != nil {
				return set, nil, fmt.Errorf("CHARTER_MODEL_PARAMS: %w", err)
			}
		}
		var prices map[string]bifrost.Price
		if p := Env("CHARTER_MODEL_PRICES"); p != "" {
			if err := json.Unmarshal([]byte(p), &prices); err != nil {
				return set, nil, fmt.Errorf("CHARTER_MODEL_PRICES must map provider[/model] to {input, cached_input, output} USD per 1M tokens: %w", err)
			}
		}
		set.Model = bifrost.New(bifrost.Config{URL: url, Key: key, Classes: classes, Params: params, Prices: prices, Tools: modelTools(set.Tools)})
	} else {
		log.Warn("model driver is a stub: CHARTER_BIFROST_URL or CHARTER_BIFROST_KEY is not set")
		set.Model = &stub.Model{}
	}

	if url, token := Env("CHARTER_MM_URL"), Env("CHARTER_MM_TOKEN"); url != "" && token != "" {
		secret := Env("CHARTER_APPROVAL_SECRET")
		if secret == "" {
			return set, nil, fmt.Errorf("CHARTER_APPROVAL_SECRET is required with the Mattermost driver")
		}
		bots, err := MMBots()
		if err != nil {
			return set, nil, err
		}
		system := Env("CHARTER_MM_SYSTEM")
		if system == "" {
			system = mattermost.DefaultSystem
		}
		set.Channels = map[string]driver.Channel{system: mattermost.New(mattermost.Config{
			URL: url, Token: token, CallbackURL: Env("CHARTER_PUBLIC_URL"), System: system,
			Secret: []byte(secret), DefaultChannel: Env("CHARTER_MM_DEFAULT_CHANNEL"),
			Bots: bots, Refusal: Env("CHARTER_MM_REFUSAL"), Log: log,
			ApproveLabel: Env("CHARTER_MM_APPROVE_LABEL"), RejectLabel: Env("CHARTER_MM_REJECT_LABEL"),
			ApprovalNote: Env("CHARTER_MM_APPROVAL_NOTE"),
			ApprovedText: Env("CHARTER_MM_APPROVED_TEXT"), RejectedText: Env("CHARTER_MM_REJECTED_TEXT"),
			ClosedText: Env("CHARTER_MM_CLOSED_TEXT"), ChangedText: Env("CHARTER_MM_CHANGED_TEXT"),
			SameApproverText: Env("CHARTER_MM_SAME_APPROVER_TEXT"), SuspendedText: Env("CHARTER_MM_SUSPENDED_TEXT")})}
	} else {
		log.Warn("channel driver is a stub: CHARTER_MM_URL or CHARTER_MM_TOKEN is not set")
		set.Channels = map[string]driver.Channel{mattermost.DefaultSystem: &stub.Channel{}}
	}
	set.Formats = map[string]driver.Format{}
	for _, f := range []driver.Format{alertmanager.Format{}} {
		set.Formats[f.Name()] = f
	}

	if url := Env("CHARTER_CH_URL"); url != "" {
		db := Env("CHARTER_CH_DATABASE")
		if db == "" {
			db = "default"
		}
		journal = clickhouse.New(clickhouse.Config{URL: url, User: Env("CHARTER_CH_USER"), Password: Env("CHARTER_CH_PASSWORD"), Database: db})
		set.Journal = journal
	} else {
		log.Warn("journal driver is a stub: CHARTER_CH_URL is not set")
		set.Journal = &stub.Journal{}
	}
	return set, journal, nil
}
