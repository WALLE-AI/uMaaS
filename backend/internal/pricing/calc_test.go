package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustRates(t *testing.T, raw string) *Rates {
	t.Helper()
	rs, err := ParseRates([]byte(raw))
	require.NoError(t, err)
	return rs
}

// 计价 §3.5 的核心判据：短请求不能被系统性截断到 0。
// 20 token 是文档里点名的例子。
func TestCharge_ShortRequestIsNotTruncatedToZero(t *testing.T) {
	v := &Version{Rates: mustRates(t, `{
		"input":  {"per_million_nano": 150000000},
		"output": {"per_million_nano": 600000000}
	}`)}
	amount, items, err := v.Charge(Usage{InputTokens: 15, OutputTokens: 5, Requests: 1})
	require.NoError(t, err)
	assert.Greater(t, amount, int64(0), "20-token request must not be truncated to zero (§3.5)")
	assert.Len(t, items, 2)
}

// 溢出边界：tokens × per_million_nano 在极端组合下会在朴素实现里溢出 int64，
// 但用 128 位累加器应当算对。
func TestCharge_LargeProductDoesNotOverflow(t *testing.T) {
	v := &Version{Rates: mustRates(t, `{
		"input":  {"per_million_nano": 1000000000000},
		"output": {"per_million_nano": 0}
	}`)}
	// 1e7 tokens * $1000/1M nano rate 的量级，文档里点名的极端组合。
	amount, _, err := v.Charge(Usage{InputTokens: 10_000_000, Requests: 1})
	require.NoError(t, err)
	// 10,000,000 * 1,000,000,000,000 / 1,000,000 = 10,000,000,000,000
	assert.Equal(t, int64(10_000_000_000_000), amount)
}

func TestCharge_RoundsHalfUpOnceOnTotal(t *testing.T) {
	// per_million_nano 选一个会产生非整除余数的值。
	v := &Version{Rates: mustRates(t, `{
		"input":  {"per_million_nano": 3},
		"output": {"per_million_nano": 0}
	}`)}
	// 1 token * 3 nano / 1e6 = 0.000003，四舍五入到 0。
	amount, _, err := v.Charge(Usage{InputTokens: 1, Requests: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(0), amount)

	// 500000 tokens * 3 / 1e6 = 1.5，四舍五入到 2（round-half-up）。
	amount2, _, err := v.Charge(Usage{InputTokens: 500_000, Requests: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), amount2)
}

// same_as 必须解析到真实费率，不能悄悄当成 0。
func TestCharge_ReasoningSameAsOutput(t *testing.T) {
	v := &Version{Rates: mustRates(t, `{
		"input":     {"per_million_nano": 100000000},
		"output":    {"per_million_nano": 400000000},
		"reasoning": {"same_as": "output"}
	}`)}
	amount, _, err := v.Charge(Usage{InputTokens: 0, OutputTokens: 0, ReasoningTokens: 1_000_000, Requests: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(400_000_000), amount)
}

func TestCharge_UndeclaredUnitIsNotCharged(t *testing.T) {
	v := &Version{Rates: mustRates(t, `{
		"input":  {"per_million_nano": 100000000},
		"output": {"per_million_nano": 400000000}
	}`)}
	// cache_read 未声明：即使传了 cache_read_tokens，也不计费。
	amount, items, err := v.Charge(Usage{InputTokens: 0, OutputTokens: 0, CacheReadTokens: 1_000_000, Requests: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(0), amount)
	for _, it := range items {
		assert.NotEqual(t, UnitCacheRead, it.Unit)
	}
}

func TestCharge_LongContextTier(t *testing.T) {
	v := &Version{Rates: mustRates(t, `{
		"input":  {"per_million_nano": 150000000},
		"output": {"per_million_nano": 600000000},
		"tiers": [
			{"above_tokens": 200000, "input_per_million_nano": 300000000, "output_per_million_nano": 1200000000}
		]
	}`)}
	// 低于阈值：按基础价。
	amount, _, err := v.Charge(Usage{InputTokens: 100_000, Requests: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(15_000_000), amount) // 100000 * 150000000 / 1e6

	// 超过阈值：input 与 output 都换成更贵的档位。
	amount2, _, err := v.Charge(Usage{InputTokens: 300_000, OutputTokens: 100_000, Requests: 1})
	require.NoError(t, err)
	expected := int64(300_000)*300_000_000/1_000_000 + int64(100_000)*1_200_000_000/1_000_000
	assert.Equal(t, expected, amount2)
}

func TestCharge_RequestFixed(t *testing.T) {
	v := &Version{Rates: mustRates(t, `{
		"input":         {"per_million_nano": 0},
		"output":        {"per_million_nano": 0},
		"request_fixed": {"per_request_nano": 500000000}
	}`)}
	amount, _, err := v.Charge(Usage{Requests: 3})
	require.NoError(t, err)
	assert.Equal(t, int64(1_500_000_000), amount)
}

func TestParseRates_RejectsUnknownUnit(t *testing.T) {
	_, err := ParseRates([]byte(`{"inptu": {"per_million_nano": 100}}`))
	require.Error(t, err, "typo'd unit names must be rejected, not silently ignored")
}

func TestParseRates_RejectsSameAsCycle(t *testing.T) {
	_, err := ParseRates([]byte(`{
		"input":  {"same_as": "output"},
		"output": {"same_as": "input"}
	}`))
	require.Error(t, err)
}

func TestParseRates_RejectsNegativeRate(t *testing.T) {
	_, err := ParseRates([]byte(`{"input": {"per_million_nano": -1}}`))
	require.Error(t, err)
}

func TestUSDPerMillionToNano_RoundTripsCleanly(t *testing.T) {
	// 0.15 在二进制浮点里不精确；round-half-up 必须落在整分位上，
	// 而不是 149999999 这种"差 1 nano"的值——这种偏差会出现在每一次调用上。
	assert.Equal(t, int64(150_000_000), USDPerMillionToNano(0.15))
	// 0.1234565 * 1e9 = 123456500，四舍五入到 nano 精度不能被截断。
	assert.Equal(t, int64(123_456_500), USDPerMillionToNano(0.1234565))
}

func TestValidate_BlocksMissingInputOutput(t *testing.T) {
	rs := mustRates(t, `{"cache_read": {"per_million_nano": 1000000}}`)
	issues := Validate(ValidateInput{Rates: rs})
	assert.True(t, HasErrors(issues))
}

func TestValidate_RequiresCacheReadWhenCapabilityDeclared(t *testing.T) {
	rs := mustRates(t, `{"input": {"per_million_nano": 150000000}, "output": {"per_million_nano": 600000000}}`)
	issues := Validate(ValidateInput{Rates: rs, Capabilities: []string{"cache"}})
	assert.True(t, HasErrors(issues))
}

func TestValidate_FlagsImplausibleMagnitude(t *testing.T) {
	// 差 1000 倍：把 "每千 token" 当成 "每百万 token" 填。
	rs := mustRates(t, `{"input": {"per_million_nano": 1000}, "output": {"per_million_nano": 600000000}}`)
	issues := Validate(ValidateInput{Rates: rs})
	assert.True(t, HasErrors(issues))
}

func TestValidate_SelfHostedRequiresPrice(t *testing.T) {
	rs := mustRates(t, `{"input": {"per_million_nano": 0}}`)
	issues := Validate(ValidateInput{Rates: rs, Hosting: "self"})
	assert.True(t, HasErrors(issues))
}

func TestValidate_RequiresNoteWhenRecentCallsExist(t *testing.T) {
	rs := mustRates(t, `{"input": {"per_million_nano": 150000000}, "output": {"per_million_nano": 600000000}}`)
	issues := Validate(ValidateInput{Rates: rs, RecentCallCount: 42, Note: ""})
	assert.True(t, HasErrors(issues))

	issuesWithNote := Validate(ValidateInput{Rates: rs, RecentCallCount: 42, Note: "涨价 10%，对标上游"})
	assert.False(t, HasErrors(issuesWithNote))
}

func TestDecodeVersion_RoundTripsThroughJSON(t *testing.T) {
	raw := []byte(`{
		"id": 7, "billing_model": "openai/gpt-5.6-sol", "scope_kind": "default",
		"scope_id": null, "currency": "USD",
		"rates": {"input": {"per_million_nano": 150000000}, "output": {"per_million_nano": 600000000}},
		"effective_from": "2026-08-14T00:00:00Z", "source": "admin", "applied_by": "admin@example.com", "note": ""
	}`)
	v, err := DecodeVersion(raw)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, int64(7), v.ID)
	nano, ok := v.Rates.PerMillionNano(UnitInput)
	assert.True(t, ok)
	assert.Equal(t, int64(150_000_000), nano)
}

func TestDecodeVersion_NullIsNil(t *testing.T) {
	v, err := DecodeVersion([]byte(`null`))
	require.NoError(t, err)
	assert.Nil(t, v)
}
