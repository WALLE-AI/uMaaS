package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider/openaicompat"
)

// TestZeroCodeOnboarding_ThreeOpenAICompatVendors 是 P2 判据的可执行版本：
//
//	"新增一个 OpenAI 兼容厂商，应当是 0 行代码"（UNIFIED-PROVIDER-INTERFACE.md §1）。
//
// 这里同时接入三家形态不同的"厂商"（用 quirks 模拟 DeepSeek/SiliconFlow 式的
// 偏离项：不给流式 usage、必须显式传 max_tokens），全部只用一份
// provider.Profile 数据和同一个 openaicompat.Driver——**没有为任何一家写
// 一行新代码**，差异全部落在 Router 的候选数据里。如果接入第三家时
// 还需要修改这个测试文件之外的任何 Go 源码，这条测试本身就会先失败
// （因为它复用的正是生产路径 CompletionsHandler → Router → openaicompat）。
func TestZeroCodeOnboarding_ThreeOpenAICompatVendors(t *testing.T) {
	type vendor struct {
		name   string
		quirks provider.Quirks
	}
	vendors := []vendor{
		{name: "deepseek-like", quirks: provider.Quirks{}},
		{name: "siliconflow-like", quirks: provider.Quirks{NoStreamUsage: true}},
		{name: "moonshot-like", quirks: provider.Quirks{MaxTokensRequired: true}},
	}

	var servers []*httptest.Server
	var candidates []RawCandidate
	for i, v := range vendors {
		v := v
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			if v.quirks.MaxTokensRequired {
				assert.Contains(t, body, "max_tokens", "%s: driver must fill max_tokens when the quirk requires it", v.name)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id": "1", "choices": []map[string]any{{
					"index": 0, "finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": fmt.Sprintf("hello from %s", v.name)},
				}},
				"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
		}))
		servers = append(servers, srv)
		candidates = append(candidates, RawCandidate{
			ChannelID: int64(i + 1), ChannelName: v.name,
			Profile:       provider.Profile{Name: v.name, BaseURL: srv.URL, Quirks: v.quirks},
			UpstreamModel: "m",
			Priority:      int32(len(vendors) - i), // 依次探测，逐一验证响应内容
		})
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	for i, cand := range candidates {
		repo := &fakeRouteRepo{
			candidates: []RawCandidate{cand},
			policy:     PolicyResolution{Strategy: "priority", FallbackDepth: 1},
		}
		router := &Router{Repo: repo, Sampler: FixedSampler{}, Now: time.Now}
		h := &CompletionsHandler{
			Models: &fakeModels{found: true, billingModel: "m"}, Router: router,
			NewDriver: func(p provider.Profile) UpstreamDoer {
				return openaicompat.New(p, http.DefaultClient)
			},
			Logs: discardLogger{}, FirstByteTimeout: 2 * time.Second, StallTimeout: 2 * time.Second,
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", chatBody("openai/m", false))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "vendor %d (%s) failed", i, vendors[i].name)
		assert.Contains(t, w.Body.String(), fmt.Sprintf("hello from %s", vendors[i].name))
	}
}
