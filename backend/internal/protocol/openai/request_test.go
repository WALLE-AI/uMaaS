package openai

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/ir"
)

func TestDecodeChatRequest_StringContent(t *testing.T) {
	body := `{"model":"gpt-x","messages":[{"role":"user","content":"hi"}]}`
	req, model, err := DecodeChatRequest(strings.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, "gpt-x", model)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "hi", req.Messages[0].PlainText())
}

func TestDecodeChatRequest_MultimodalContent(t *testing.T) {
	body := `{"model":"gpt-x","messages":[{"role":"user","content":[
		{"type":"text","text":"what is this"},
		{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}
	]}]}`
	req, _, err := DecodeChatRequest(strings.NewReader(body))
	require.NoError(t, err)
	require.Len(t, req.Messages[0].Content, 2)
	assert.Equal(t, "image_url", req.Messages[0].Content[1].Type)
	assert.Equal(t, "https://example.com/a.png", req.Messages[0].Content[1].ImageURL)
}

func TestDecodeChatRequest_RejectsMissingModel(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	_, _, err := DecodeChatRequest(strings.NewReader(body))
	require.Error(t, err)
}

func TestDecodeChatRequest_RejectsEmptyMessages(t *testing.T) {
	body := `{"model":"gpt-x","messages":[]}`
	_, _, err := DecodeChatRequest(strings.NewReader(body))
	require.Error(t, err)
}

func TestDecodeChatRequest_StopAsStringOrArray(t *testing.T) {
	req, _, err := DecodeChatRequest(strings.NewReader(
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"stop":"END"}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"END"}, req.Stop)

	req2, _, err := DecodeChatRequest(strings.NewReader(
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"stop":["A","B"]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B"}, req2.Stop)
}

func TestDecodeChatRequest_StreamOptionsIncludeUsage(t *testing.T) {
	req, _, err := DecodeChatRequest(strings.NewReader(
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`))
	require.NoError(t, err)
	assert.True(t, req.Stream)
	assert.True(t, req.StreamUsage)
}

func TestEncodeUpstreamRequest_OverridesModel(t *testing.T) {
	req := &ir.Request{Messages: []ir.Message{ir.Text(ir.RoleUser, "hi")}}
	raw, err := EncodeUpstreamRequest(req, "upstream-actual-name")
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"model":"upstream-actual-name"`)
}

func TestResponseRoundTrip_NonStream(t *testing.T) {
	upstreamBody := `{
		"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"upstream-model",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
	}`
	resp, err := DecodeResponse(strings.NewReader(upstreamBody), "requested-model")
	require.NoError(t, err)
	assert.Equal(t, ir.FinishStop, resp.FinishReason)
	assert.Equal(t, int64(3), resp.Usage.PromptTokens)
	assert.Equal(t, ir.UsageUpstream, resp.UsageSource)

	var buf bytes.Buffer
	require.NoError(t, EncodeResponse(&buf, resp, "requested-model"))
	out := buf.String()
	assert.Contains(t, out, `"model":"requested-model"`)
	assert.NotContains(t, out, "upstream-model")
	assert.Contains(t, out, `"content":"hi there"`)
}

func TestResponseRoundTrip_MissingUsageIsEstimated(t *testing.T) {
	upstreamBody := `{
		"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]
	}`
	resp, err := DecodeResponse(strings.NewReader(upstreamBody), "m")
	require.NoError(t, err)
	assert.Equal(t, ir.UsageEstimated, resp.UsageSource)
}

func TestDecodeResponse_RejectsEmptyChoices(t *testing.T) {
	_, err := DecodeResponse(strings.NewReader(`{"id":"1","choices":[]}`), "m")
	require.Error(t, err)
}
