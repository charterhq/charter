// Package bifrost runs model steps through a Bifrost gateway (OpenAI-compatible API).
package bifrost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charterhq/charter/internal/driver"
)

// Config of the driver. Classes maps a platform model class to a gateway model or alias.
type Config struct {
	URL     string
	Key     string
	Classes map[string]string
	// Params are extra request fields per class, e.g. max_tokens or reasoning settings.
	Params map[string]map[string]any
	// Prices price a call when the gateway does not return its cost, keyed by "provider/model"
	// or by "provider". Without a price the cost stays 0 and the run record shows it.
	Prices map[string]Price
	HTTP   *http.Client
}

// Price is USD per million tokens.
type Price struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cached_input"`
	Output      float64 `json:"output"`
}

// cost of a call. Cached tokens are part of the input count and are priced at their own rate.
func (p Price) cost(u driver.Usage) float64 {
	cached := min(u.CachedTokens, u.InputTokens)
	return (float64(u.InputTokens-cached)*p.Input + float64(cached)*p.CachedInput + float64(u.OutputTokens)*p.Output) / 1e6
}

func (m *Model) price(provider, model string) (Price, bool) {
	if p, ok := m.cfg.Prices[provider+"/"+model]; ok {
		return p, true
	}
	p, ok := m.cfg.Prices[provider]
	return p, ok
}

// gatewayCost reads usage.cost, which the gateway returns either as a number or as {"total_cost": n},
// and only for some providers.
func gatewayCost(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var o struct {
		TotalCost *float64 `json:"total_cost"`
	}
	if json.Unmarshal(raw, &o) == nil && o.TotalCost != nil {
		return *o.TotalCost, true
	}
	return 0, false
}

// Model is a driver.ModelStep backed by Bifrost.
type Model struct {
	cfg Config
}

// New returns a model driver.
func New(cfg Config) *Model {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 10 * time.Minute}
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Model{cfg: cfg}
}

// ErrToolsUnsupported is returned for steps that need the tool loop, which is not implemented yet.
var ErrToolsUnsupported = errors.New("bifrost driver: tool loop (level 3) is not implemented yet")

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
		Cost json.RawMessage `json:"cost"`
	} `json:"usage"`
	ExtraFields struct {
		Provider string `json:"provider"`
	} `json:"extra_fields"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Run makes one model call and returns the JSON the model produced.
func (m *Model) Run(ctx context.Context, req driver.StepRequest) (driver.StepResult, error) {
	if len(req.Tools) > 0 {
		return driver.StepResult{}, ErrToolsUnsupported
	}
	model, ok := m.cfg.Classes[req.ModelClass]
	if !ok {
		return driver.StepResult{}, fmt.Errorf("bifrost driver: no model for class %q", req.ModelClass)
	}

	system := req.Instructions
	if len(req.OutputSchema) > 0 {
		system += "\n\nReturn only one JSON object that matches this JSON Schema:\n" + string(req.OutputSchema)
	}
	msgs := []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: string(req.Input)},
	}
	if len(req.Feedback) > 0 {
		msgs = append(msgs, chatMessage{Role: "user", Content: "The previous answer failed validation:\n- " +
			strings.Join(req.Feedback, "\n- ") + "\nReturn the corrected JSON only."})
	}
	body := map[string]any{
		"model":           model,
		"messages":        msgs,
		"response_format": map[string]string{"type": "json_object"},
	}
	// Platform defaults for the class first, then the agent's own parameters from its package.
	for k, v := range m.cfg.Params[req.ModelClass] {
		body[k] = v
	}
	for k, v := range req.Params {
		switch k {
		case "temperature", "top_p", "seed":
			body[k] = v
		case "max_output_tokens", "max_tokens":
			body["max_tokens"] = v
		case "reasoning":
			body["reasoning_effort"] = v
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return driver.StepResult{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.URL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return driver.StepResult{}, err
	}
	hreq.Header.Set("Authorization", "Bearer "+m.cfg.Key)
	hreq.Header.Set("Content-Type", "application/json")
	// Attribution of the call to the run in gateway logs.
	hreq.Header.Set("x-bf-session-id", req.RunID)
	hreq.Header.Set("x-bf-lh-agent", req.Agent)
	hreq.Header.Set("x-bf-lh-scenario", req.Scenario)
	if req.Traceparent != "" {
		hreq.Header.Set("traceparent", req.Traceparent)
	}

	resp, err := m.cfg.HTTP.Do(hreq)
	if err != nil {
		return driver.StepResult{}, fmt.Errorf("bifrost: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return driver.StepResult{}, fmt.Errorf("bifrost: read response: %w", err)
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return driver.StepResult{}, fmt.Errorf("bifrost: HTTP %d, undecodable response: %.200s", resp.StatusCode, raw)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("%.200s", raw)
		if cr.Error != nil {
			msg = cr.Error.Message
		}
		return driver.StepResult{}, fmt.Errorf("bifrost: HTTP %d: %s", resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return driver.StepResult{}, errors.New("bifrost: response has no choices")
	}

	usage := driver.Usage{
		InputTokens:     cr.Usage.PromptTokens,
		OutputTokens:    cr.Usage.CompletionTokens,
		CachedTokens:    cr.Usage.PromptTokensDetails.CachedTokens,
		ReasoningTokens: cr.Usage.CompletionTokensDetails.ReasoningTokens,
		Model:           cr.Model,
		Provider:        cr.ExtraFields.Provider,
		FinishReason:    cr.Choices[0].FinishReason,
	}
	if c, ok := gatewayCost(cr.Usage.Cost); ok {
		usage.Cost = c
	} else if p, ok := m.price(usage.Provider, usage.Model); ok {
		usage.Cost = p.cost(usage)
	}
	out := extractJSON(cr.Choices[0].Message.Content)
	return driver.StepResult{Output: json.RawMessage(out), Usage: usage}, nil
}

// extractJSON strips a markdown code fence around the answer, if the model added one.
// Whatever remains goes to schema validation as is.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	if json.Valid([]byte(s)) {
		return s
	}
	// Not JSON: wrap as a JSON string so the run record stays valid JSON and validation reports it.
	b, _ := json.Marshal(s)
	return string(b)
}
