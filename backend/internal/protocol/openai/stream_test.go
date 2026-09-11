package openai

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

func readAll(t *testing.T, r *StreamReader) []ir.StreamEvent {
	t.Helper()
	var events []ir.StreamEvent
	for {
		ev, err := r.Next()
		if ev != nil {
			events = append(events, ev)
		}
		if err != nil {
			require.ErrorIs(t, err, ErrStreamDone, "unexpected stream error: %v", err)
			return events
		}
	}
}

func TestStreamReader_TextOnly(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","model":"gpt-x","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"Hel"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	r := NewStreamReader(strings.NewReader(body), false)
	events := readAll(t, r)

	var texts []string
	var sawStart, sawBlockStart, sawBlockStop, sawDelta, sawStop bool
	for _, ev := range events {
		switch e := ev.(type) {
		case ir.EventMessageStart:
			sawStart = true
		case ir.EventBlockStart:
			sawBlockStart = true
			assert.Equal(t, ir.BlockText, e.Kind)
		case ir.EventBlockDelta:
			texts = append(texts, e.Text)
		case ir.EventBlockStop:
			sawBlockStop = true
		case ir.EventMessageDelta:
			sawDelta = true
			assert.Equal(t, ir.FinishStop, e.FinishReason)
		case ir.EventMessageStop:
			sawStop = true
		case ir.EventError:
			t.Fatalf("unexpected error event: %+v", e)
		}
	}
	assert.True(t, sawStart && sawBlockStart && sawBlockStop && sawDelta && sawStop)
	assert.Equal(t, "Hello", strings.Join(texts, ""))
}

func TestStreamReader_WaitsForUsageChunkWhenRequested(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	r := NewStreamReader(strings.NewReader(body), true)
	events := readAll(t, r)

	var delta *ir.EventMessageDelta
	for _, ev := range events {
		if d, ok := ev.(ir.EventMessageDelta); ok {
			delta = &d
		}
	}
	require.NotNil(t, delta)
	require.NotNil(t, delta.Usage)
	assert.Equal(t, int64(5), delta.Usage.PromptTokens)
	assert.Equal(t, ir.UsageUpstream, delta.UsageSource)
}

func TestStreamReader_MissingUsageChunkFallsBackToEstimated(t *testing.T) {
	// 客户端要了 usage，但上游是个不老实的 quirk 厂商，
	// finish_reason 之后直接 [DONE] 没给 usage chunk。
	body := strings.Join([]string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	r := NewStreamReader(strings.NewReader(body), true)
	events := readAll(t, r)

	var delta *ir.EventMessageDelta
	for _, ev := range events {
		if d, ok := ev.(ir.EventMessageDelta); ok {
			delta = &d
		}
	}
	require.NotNil(t, delta)
	assert.Nil(t, delta.Usage)
	assert.Equal(t, ir.UsageEstimated, delta.UsageSource)
}

func TestStreamReader_ToolCallArgumentsAccumulateAcrossChunks(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"nyc\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	r := NewStreamReader(strings.NewReader(body), false)
	events := readAll(t, r)

	var toolName, toolID string
	var argsParts []string
	var sawBlockStart bool
	for _, ev := range events {
		switch e := ev.(type) {
		case ir.EventBlockStart:
			if e.Kind == ir.BlockToolUse {
				sawBlockStart = true
				toolName, toolID = e.ToolName, e.ToolCallID
			}
		case ir.EventBlockDelta:
			if e.PartialJSON != "" {
				argsParts = append(argsParts, e.PartialJSON)
			}
		}
	}
	assert.True(t, sawBlockStart)
	assert.Equal(t, "get_weather", toolName)
	assert.Equal(t, "call_1", toolID)
	assert.Equal(t, `{"city":"nyc"}`, strings.Join(argsParts, ""))
}

func TestStreamReader_UpstreamClosedWithoutDoneReportsError(t *testing.T) {
	// 连接中途被切断：没有 finish_reason，也没有 [DONE]。
	body := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}` + "\n"

	r := NewStreamReader(strings.NewReader(body), false)
	events := readAll(t, r)

	var sawError bool
	for _, ev := range events {
		if e, ok := ev.(ir.EventError); ok {
			sawError = true
			assert.NotEmpty(t, e.Message)
		}
		// 必须不能出现一个假的 stop
		if d, ok := ev.(ir.EventMessageDelta); ok {
			assert.NotEqual(t, ir.FinishStop, d.FinishReason,
				"an abruptly closed stream must never be reported as finish_reason: stop")
		}
	}
	assert.True(t, sawError, "abrupt close without [DONE] must surface as EventError")
}

func TestStreamWriter_RoundTripsTextThroughOpenAIShape(t *testing.T) {
	var buf bytes.Buffer
	w := NewStreamWriter(&buf, "requested-model-name")

	events := []ir.StreamEvent{
		ir.EventMessageStart{ID: "abc", Model: "upstream-model", Role: ir.RoleAssistant},
		ir.EventBlockStart{Index: 0, Kind: ir.BlockText},
		ir.EventBlockDelta{Index: 0, Text: "Hello"},
		ir.EventBlockStop{Index: 0},
		ir.EventMessageDelta{FinishReason: ir.FinishStop},
		ir.EventMessageStop{},
	}
	for _, ev := range events {
		require.NoError(t, w.Write(ev))
	}

	out := buf.String()
	assert.Contains(t, out, `"model":"requested-model-name"`)
	assert.NotContains(t, out, "upstream-model", "the wire model field must echo the client's requested name, not the upstream one")
	assert.Contains(t, out, `"content":"Hello"`)
	assert.Contains(t, out, `"finish_reason":"stop"`)
	assert.True(t, strings.HasSuffix(out, "data: [DONE]\n\n"))
}

func TestStreamWriter_ErrorEventNeverEmitsFinishStop(t *testing.T) {
	var buf bytes.Buffer
	w := NewStreamWriter(&buf, "m")
	require.NoError(t, w.Write(ir.EventError{Code: "stall_timeout", Message: "upstream stalled"}))

	out := buf.String()
	assert.Contains(t, out, `"upstream_error"`)
	assert.NotContains(t, out, `"finish_reason":"stop"`,
		"an error event must never look like a normal stop to the client SDK")
	assert.True(t, strings.HasSuffix(out, "data: [DONE]\n\n"))
}

func TestStreamWriter_PanicsIfAskedToEncodeFinishError(t *testing.T) {
	var buf bytes.Buffer
	w := NewStreamWriter(&buf, "m")
	defer func() {
		r := recover()
		assert.NotNil(t, r, "encoding FinishError as a normal finish_reason must be caught, not silently mapped to stop")
	}()
	_ = w.Write(ir.EventMessageDelta{FinishReason: ir.FinishError})
}

func TestErrStreamDone_IsErrorsIsCompatible(t *testing.T) {
	err := ErrStreamDone
	assert.True(t, errors.Is(err, ErrStreamDone))
}
