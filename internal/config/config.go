// Package config builds the core configuration and drivers from the environment.
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/charterhq/charter/drivers/bifrost"
	"github.com/charterhq/charter/drivers/clickhouse"
	"github.com/charterhq/charter/drivers/mattermost"
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
		out[tok] = name
	}
	return out, nil
}

// Drivers builds the driver set. A driver whose settings are missing falls back to a stub, with a warning.
func Drivers(log *slog.Logger) (driver.Set, *clickhouse.Journal, error) {
	var set driver.Set
	var journal *clickhouse.Journal

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
		set.Model = bifrost.New(bifrost.Config{URL: url, Key: key, Classes: classes, Params: params, Prices: prices})
	} else {
		log.Warn("model driver is a stub: CHARTER_BIFROST_URL or CHARTER_BIFROST_KEY is not set")
		set.Model = &stub.Model{}
	}

	if url, token := Env("CHARTER_MM_URL"), Env("CHARTER_MM_TOKEN"); url != "" && token != "" {
		secret := Env("CHARTER_APPROVAL_SECRET")
		if secret == "" {
			return set, nil, fmt.Errorf("CHARTER_APPROVAL_SECRET is required with the Mattermost driver")
		}
		set.Channel = mattermost.New(mattermost.Config{URL: url, Token: token, CallbackURL: Env("CHARTER_PUBLIC_URL"),
			Secret: []byte(secret), DefaultChannel: Env("CHARTER_MM_DEFAULT_CHANNEL")})
	} else {
		log.Warn("channel driver is a stub: CHARTER_MM_URL or CHARTER_MM_TOKEN is not set")
		set.Channel = &stub.Channel{}
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

	set.Tools = &stub.Tools{} // MCP gateway driver comes with the tools step
	return set, journal, nil
}
