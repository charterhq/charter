package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/charterhq/charter/drivers/mcp"
)

type searchIn struct {
	JQL string `json:"jql"`
}

// gateway is an MCP server named like Bifrost names server tools ("jira-..."), behind a bearer key.
func gateway(t *testing.T) (*httptest.Server, *[]string) {
	srv := sdk.NewServer(&sdk.Implementation{Name: "gw"}, nil)
	sdk.AddTool(srv, &sdk.Tool{Name: "jira-jira_search", Description: "search"}, func(_ context.Context, _ *sdk.CallToolRequest, in searchIn) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found for " + in.JQL}}}, nil, nil
	})
	sdk.AddTool(srv, &sdk.Tool{Name: "jira-jira_get_issue"}, func(_ context.Context, _ *sdk.CallToolRequest, _ map[string]any) (*sdk.CallToolResult, any, error) {
		return nil, nil, errors.New("issue not found")
	})
	sdk.AddTool(srv, &sdk.Tool{Name: "gitlab-get_mr"}, func(_ context.Context, _ *sdk.CallToolRequest, _ map[string]any) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{}, map[string]any{"iid": 7}, nil
	})
	var keys []string
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		keys = append(keys, auth)
		if auth != "Bearer k-triage" && auth != "Bearer k-default" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts, &keys
}

func TestListAndCall(t *testing.T) {
	ts, keys := gateway(t)
	tools := mcp.New(mcp.Config{URL: ts.URL, Keys: map[string]string{"triage": "k-triage", "*": "k-default"}})
	defer func() { _ = tools.Close() }()
	ctx := context.Background()

	defs, err := tools.List(ctx, "triage", []string{"jira.*", "gitlab.get_mr"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	if strings.Join(names, ",") != "jira.jira_get_issue,jira.jira_search,gitlab.get_mr" {
		t.Fatalf("names %v", names)
	}
	if !strings.Contains(string(defs[1].InputSchema), "jql") || defs[1].Description != "search" {
		t.Fatalf("def %+v", defs[1])
	}

	if _, err := tools.List(ctx, "triage", []string{"jira.jira_delete"}); err == nil || !strings.Contains(err.Error(), "jira.jira_delete") {
		t.Fatalf("missing tool not reported: %v", err)
	}

	out, err := tools.Call(ctx, "triage", "jira.jira_search", json.RawMessage(`{"jql":"project = SRE"}`))
	if err != nil || string(out) != `"found for project = SRE"` {
		t.Fatalf("call %s %v", out, err)
	}
	out, err = tools.Call(ctx, "other", "gitlab.get_mr", nil)
	if err != nil || string(out) != `{"iid":7}` {
		t.Fatalf("structured %s %v", out, err)
	}
	var te *mcp.ToolError
	if _, err := tools.Call(ctx, "triage", "jira.jira_get_issue", json.RawMessage(`{}`)); !errors.As(err, &te) || !strings.Contains(te.Text, "issue not found") {
		t.Fatalf("tool error %v", err)
	}
	seen := map[string]bool{}
	for _, k := range *keys {
		seen[k] = true
	}
	if !seen["Bearer k-triage"] || !seen["Bearer k-default"] || len(seen) != 2 {
		t.Fatalf("keys %v", seen)
	}
}

func TestNoKey(t *testing.T) {
	tools := mcp.New(mcp.Config{URL: "http://127.0.0.1:1", Keys: map[string]string{"a": "k"}})
	if _, err := tools.List(context.Background(), "b", []string{"x.y"}); !errors.Is(err, mcp.ErrNoKey) {
		t.Fatalf("err %v", err)
	}
}

// Live gateway: CHARTER_MCP_URL, CHARTER_MCP_KEY, CHARTER_MCP_LIVE_TOOL ("ref.function", called without arguments).
func TestLive(t *testing.T) {
	url, key, tool := os.Getenv("CHARTER_MCP_URL"), os.Getenv("CHARTER_MCP_KEY"), os.Getenv("CHARTER_MCP_LIVE_TOOL")
	if url == "" || key == "" || tool == "" {
		t.Skip("live gateway is not configured")
	}
	tools := mcp.New(mcp.Config{URL: url, Keys: map[string]string{"*": key}})
	defer func() { _ = tools.Close() }()
	ref, _, _ := strings.Cut(tool, ".")
	defs, err := tools.List(context.Background(), "live", []string{ref + ".*"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d tools of %s", len(defs), ref)
	out, err := tools.Call(context.Background(), "live", tool, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %.300s", tool, out)
}
