package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newEcho() *Server {
	tools := []Tool{
		{
			Name:        "echo",
			Description: "echo input back",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		},
	}
	h := func(ctx context.Context, name string, args json.RawMessage) (Result, error) {
		if name != "echo" {
			return ErrorResult("unknown tool: " + name), nil
		}
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return ErrorResult("bad args"), nil
		}
		return TextResult("got: " + p.Text), nil
	}
	return New("test", "0.0.1", tools, h)
}

func TestMCP_initializeAndListTools(t *testing.T) {
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n",
	)
	var out bytes.Buffer
	s := newEcho()
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d response lines, want 2:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion"`) {
		t.Errorf("init response missing protocolVersion: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"name":"test"`) {
		t.Errorf("init response missing serverInfo.name: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"echo"`) {
		t.Errorf("tools/list missing 'echo': %s", lines[1])
	}
}

func TestMCP_callTool(t *testing.T) {
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}` + "\n",
	)
	var out bytes.Buffer
	s := newEcho()
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `got: hi`) {
		t.Errorf("missing echoed text: %s", got)
	}
	if !strings.Contains(got, `"id":3`) {
		t.Errorf("missing id correlation: %s", got)
	}
}

func TestMCP_unknownMethodError(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"unknown/method"}` + "\n")
	var out bytes.Buffer
	s := newEcho()
	_ = s.Serve(context.Background(), in, &out)
	if !strings.Contains(out.String(), `method not found`) {
		t.Errorf("missing method-not-found: %s", out.String())
	}
	if !strings.Contains(out.String(), `-32601`) {
		t.Errorf("missing JSON-RPC error code -32601: %s", out.String())
	}
}

func TestMCP_notificationNoResponse(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var out bytes.Buffer
	s := newEcho()
	_ = s.Serve(context.Background(), in, &out)
	if out.Len() != 0 {
		t.Errorf("notification produced response: %s", out.String())
	}
}

func TestMCP_malformedJSONReturnsParseError(t *testing.T) {
	in := strings.NewReader("not json at all\n")
	var out bytes.Buffer
	s := newEcho()
	_ = s.Serve(context.Background(), in, &out)
	if !strings.Contains(out.String(), `parse error`) {
		t.Errorf("missing parse error: %s", out.String())
	}
}

func TestMCP_pingReturnsEmpty(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"ping"}` + "\n")
	var out bytes.Buffer
	s := newEcho()
	_ = s.Serve(context.Background(), in, &out)
	if !strings.Contains(out.String(), `"id":5`) {
		t.Errorf("ping missing id correlation: %s", out.String())
	}
	if !strings.Contains(out.String(), `"result":{}`) {
		t.Errorf("ping missing empty result: %s", out.String())
	}
}
