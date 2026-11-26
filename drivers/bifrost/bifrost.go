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
	// Tools runs the tool calls of steps that have tools; without it such steps fail.
	Tools driver.Tools
	HTTP  *http.Client
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

// ErrNoTools means a step asks for tools but the core runs without a tools driver.
var ErrNoTools = errors.New("bifrost driver: the step needs tools, but no tools driver is configured (CHARTER_MCP_URL)")

// Default step budget of the tool loop when the package sets none.
const (
	defaultMaxSteps     = 8
	defaultMaxToolCalls = 20
)

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
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

// fnName turns a package tool name into a function name models accept ([a-zA-Z0-9_-]).
func fnName(pkgName string) string { return strings.ReplaceAll(pkgName, ".", "__") }

// Run makes one model call, or runs the model with tools until it answers, within the step budget.
// It returns the JSON the model produced.
func (m *Model) Run(ctx context.Context, req driver.StepRequest) (driver.StepResult, error) {
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

	var tools []map[string]any
	byFn := map[string]string{}
	if len(req.Tools) > 0 {
		if m.cfg.Tools == nil {
			return driver.StepResult{}, ErrNoTools
		}
		defs, err := m.cfg.Tools.List(ctx, req.Agent, req.Tools)
		if err != nil {
			return driver.StepResult{}, err
		}
		for _, d := range defs {
			fn := fnName(d.Name)
			byFn[fn] = d.Name
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": fn, "description": d.Description, "parameters": d.InputSchema,
			}})
		}
	}
	maxSteps, maxCalls := req.MaxSteps, req.MaxToolCalls
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	if maxCalls <= 0 {
		maxCalls = defaultMaxToolCalls
	}
	if len(tools) == 0 {
		maxSteps = 1
	}

	var usage driver.Usage
	for step := 1; ; step++ {
		// The last step and an exhausted call budget leave the model no tools: it must answer.
		offer := len(tools) > 0 && step < maxSteps && usage.ToolCalls < maxCalls
		cr, err := m.call(ctx, req, model, msgs, tools, offer)
		if err != nil {
			return driver.StepResult{Usage: usage}, err
		}
		m.addUsage(&usage, cr)
		choice := cr.Choices[0]
		if len(choice.Message.ToolCalls) == 0 || !offer {
			out := extractJSON(choice.Message.Content)
			return driver.StepResult{Output: json.RawMessage(out), Usage: usage}, nil
		}
		msgs = append(msgs, chatMessage{Role: "assistant", Content: choice.Message.Content, ToolCalls: choice.Message.ToolCalls})
		for _, tc := range choice.Message.ToolCalls {
			msgs = append(msgs, chatMessage{Role: "tool", ToolCallID: tc.ID, Content: m.runTool(ctx, req.Agent, byFn, tc, &usage, maxCalls)})
		}
	}
}

// runTool executes one call the model asked for and returns what the model gets back.
// Failures go back to the model as text: it may fix the arguments or answer without the tool.
func (m *Model) runTool(ctx context.Context, agent string, byFn map[string]string, tc toolCall, usage *driver.Usage, maxCalls int) string {
	name, ok := byFn[tc.Function.Name]
	if !ok {
		usage.ToolErrors++
		return fmt.Sprintf("error: tool %q is not available in this step", tc.Function.Name)
	}
	if usage.ToolCalls >= maxCalls {
		usage.ToolErrors++
		return "error: tool call budget of the run is spent, answer with what you have"
	}
	usage.ToolCalls++
	args := json.RawMessage(tc.Function.Arguments)
	if len(strings.TrimSpace(tc.Function.Arguments)) == 0 {
		args = json.RawMessage("{}")
	}
	res, err := m.cfg.Tools.Call(ctx, agent, name, args)
	if err != nil {
		usage.ToolErrors++
		return "error: " + err.Error()
	}
	return string(res)
}

func (m *Model) addUsage(u *driver.Usage, cr chatResponse) {
	call := driver.Usage{
		InputTokens:     cr.Usage.PromptTokens,
		OutputTokens:    cr.Usage.CompletionTokens,
		CachedTokens:    cr.Usage.PromptTokensDetails.CachedTokens,
		ReasoningTokens: cr.Usage.CompletionTokensDetails.ReasoningTokens,
		Model:           cr.Model,
		Provider:        cr.ExtraFields.Provider,
	}
	if c, ok := gatewayCost(cr.Usage.Cost); ok {
		call.Cost = c
	} else if p, ok := m.price(call.Provider, call.Model); ok {
		call.Cost = p.cost(call)
	}
	u.InputTokens += call.InputTokens
	u.OutputTokens += call.OutputTokens
	u.CachedTokens += call.CachedTokens
	u.ReasoningTokens += call.ReasoningTokens
	u.Cost += call.Cost
	u.Model, u.Provider = call.Model, call.Provider
	u.FinishReason = cr.Choices[0].FinishReason
}

// call makes one chat completion request.
func (m *Model) call(ctx context.Context, req driver.StepRequest, model string, msgs []chatMessage, tools []map[string]any, offer bool) (chatResponse, error) {
	body := map[string]any{
		"model":           model,
		"messages":        msgs,
		"response_format": map[string]string{"type": "json_object"},
	}
	if len(tools) > 0 {
		// Tools stay in every request of the loop (stable prompt prefix for the gateway cache);
		// tool_choice says whether the model may still call them.
		body["tools"] = tools
		if offer {
			body["tool_choice"] = "auto"
		} else {
			body["tool_choice"] = "none"
		}
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
		return chatResponse{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.URL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return chatResponse{}, err
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
		return chatResponse{}, fmt.Errorf("bifrost: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return chatResponse{}, fmt.Errorf("bifrost: read response: %w", err)
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatResponse{}, fmt.Errorf("bifrost: HTTP %d, undecodable response: %.200s", resp.StatusCode, raw)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("%.200s", raw)
		if cr.Error != nil {
			msg = cr.Error.Message
		}
		return chatResponse{}, fmt.Errorf("bifrost: HTTP %d: %s", resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return chatResponse{}, errors.New("bifrost: response has no choices")
	}
	return cr, nil
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
