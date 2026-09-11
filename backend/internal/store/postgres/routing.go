package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/gateway"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/provider"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres/dbgen"
)

// RoutingRepo 实现 P3 的渠道候选查询与 X9 的策略解析。
//
// **这是 P2 判据的落地处**：新增一个 OpenAI 兼容厂商 = `provider_profiles`
// 插一行 + `channels` 插一行 + `channel_models` 插几行，这个仓储的读法
// 不需要因为多了一个厂商而改动一处代码。
type RoutingRepo struct {
	q      *dbgen.Queries
	cipher *platform.Cipher
}

func NewRoutingRepo(db *store.DB, cipher *platform.Cipher) *RoutingRepo {
	return &RoutingRepo{q: dbgen.New(db.Pool), cipher: cipher}
}

var _ gateway.RouteRepository = (*RoutingRepo)(nil)

func (r *RoutingRepo) ListCandidates(ctx context.Context, modelID int64) ([]gateway.RawCandidate, error) {
	rows, err := r.q.ListCandidateChannels(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("list candidate channels: %w", err)
	}
	out := make([]gateway.RawCandidate, 0, len(rows))
	for _, row := range rows {
		apiKey, err := r.decryptCredentials(row.CredentialsEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials for channel %d: %w", row.ChannelID, err)
		}
		quirks, err := decodeQuirks(row.Quirks)
		if err != nil {
			return nil, err
		}
		baseURL := row.ProviderBaseUrl
		if row.BaseUrlOverride != nil && *row.BaseUrlOverride != "" {
			baseURL = *row.BaseUrlOverride
		}
		priority, weight := row.Priority, row.Weight
		if row.PriorityOverride != nil {
			priority = *row.PriorityOverride
		}
		if row.WeightOverride != nil {
			weight = *row.WeightOverride
		}

		out = append(out, gateway.RawCandidate{
			ChannelID: row.ChannelID, ChannelName: row.ChannelName,
			Profile: provider.Profile{
				Name: row.ChannelName, BaseURL: baseURL,
				AuthScheme: provider.AuthScheme(row.AuthScheme), AuthHeader: row.AuthHeader,
				APIKey: apiKey, Quirks: quirks,
			},
			UpstreamModel: row.UpstreamModelName,
			Priority:      priority, Weight: weight,
			Health: gateway.ChannelHealth{
				Alpha: row.HealthAlpha, Beta: row.HealthBeta,
				LatencyEMAMs: row.LatencyEmaMs, ConsecutiveFailures: row.ConsecutiveFailures,
				RateLimitedUntil: row.RateLimitedUntil,
			},
		})
	}
	return out, nil
}

func (r *RoutingRepo) ResolvePolicy(ctx context.Context, modelID int64, workspaceID string) (gateway.PolicyResolution, error) {
	modelIDStr := strconv.FormatInt(modelID, 10)
	params := dbgen.ResolveRoutingPolicyParams{ModelID: &modelIDStr}
	if workspaceID != "" {
		params.WorkspaceID = &workspaceID
	}
	row, err := r.q.ResolveRoutingPolicy(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 没有任何策略配置过：退回 balanced，而不是报错——
			// 路由策略是可选的运维精调项，不该成为"没配置就打不通"的前提。
			return gateway.PolicyResolution{Strategy: "balanced", FallbackDepth: 3}, nil
		}
		return gateway.PolicyResolution{}, fmt.Errorf("resolve routing policy: %w", err)
	}
	res := gateway.PolicyResolution{Strategy: row.Strategy, FallbackDepth: int(row.FallbackDepth)}
	if row.Strategy == "custom" && len(row.Weights) > 0 {
		var w gateway.RoutingWeights
		if err := json.Unmarshal(row.Weights, &w); err != nil {
			return gateway.PolicyResolution{}, fmt.Errorf("decode routing weights: %w", err)
		}
		res.Weights = &w
	}
	return res, nil
}

func (r *RoutingRepo) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64) error {
	if err := r.q.RecordChannelSuccess(ctx, dbgen.RecordChannelSuccessParams{
		ID: channelID, LatencyMs: latencyMs,
	}); err != nil {
		return fmt.Errorf("record channel success: %w", err)
	}
	return nil
}

func (r *RoutingRepo) RecordFailure(ctx context.Context, channelID int64, rateLimited bool, rateLimitSeconds int) error {
	if err := r.q.RecordChannelFailure(ctx, dbgen.RecordChannelFailureParams{
		ID: channelID, RateLimited: rateLimited, RateLimitSeconds: int32(rateLimitSeconds),
	}); err != nil {
		return fmt.Errorf("record channel failure: %w", err)
	}
	return nil
}

func (r *RoutingRepo) decryptCredentials(enc []byte) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	if r.cipher == nil {
		return "", fmt.Errorf("security.encryption_key is not configured; cannot decrypt channel credentials")
	}
	plain, err := r.cipher.Decrypt(enc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func decodeQuirks(raw []byte) (provider.Quirks, error) {
	var q provider.Quirks
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return q, nil
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return q, fmt.Errorf("decode provider quirks: %w", err)
	}
	return q, nil
}

// ── admin/seed 写入口：新增供应商与渠道 ─────────────────────────
//
// 没有 HTTP 端点（那是 I6 的 B6b），但要让 P2 的判据可验证——"接入
// DeepSeek/Moonshot/SiliconFlow 零代码"——就必须有一条写路径能把
// provider_profiles/channels/channel_models 灌进去，且过程中不写
// 任何 Go 代码。umaas seed 用这条路径。

type ProviderProfileInput struct {
	Slug, Name, Protocol, BaseURL string
	AuthScheme, AuthHeader        string
	Quirks                        provider.Quirks
}

func (r *RoutingRepo) UpsertProviderProfile(ctx context.Context, in ProviderProfileInput) (int64, error) {
	quirks, err := json.Marshal(in.Quirks)
	if err != nil {
		return 0, err
	}
	row, err := r.q.UpsertProviderProfile(ctx, dbgen.UpsertProviderProfileParams{
		Slug: in.Slug, Name: in.Name, Protocol: in.Protocol, BaseUrl: in.BaseURL,
		AuthScheme: in.AuthScheme, AuthHeader: in.AuthHeader, Quirks: quirks,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert provider profile: %w", err)
	}
	return row.ID, nil
}

type ChannelInput struct {
	ProviderProfileID int64
	Name              string
	BaseURLOverride   string
	APIKey            string
	Region            string
	Priority, Weight  int32
}

func (r *RoutingRepo) CreateChannel(ctx context.Context, in ChannelInput) (int64, error) {
	if r.cipher == nil {
		return 0, fmt.Errorf("security.encryption_key is not configured; cannot store channel credentials")
	}
	enc, err := r.cipher.Encrypt([]byte(in.APIKey))
	if err != nil {
		return 0, err
	}
	var override *string
	if in.BaseURLOverride != "" {
		override = &in.BaseURLOverride
	}
	row, err := r.q.CreateChannel(ctx, dbgen.CreateChannelParams{
		ProviderProfileID: in.ProviderProfileID, Name: in.Name, BaseUrlOverride: override,
		CredentialsEnc: enc, Region: in.Region, Priority: in.Priority, Weight: in.Weight,
		Status: "active",
	})
	if err != nil {
		return 0, fmt.Errorf("create channel: %w", err)
	}
	return row.ID, nil
}

type ChannelModelInput struct {
	ChannelID         int64
	ModelID           int64
	UpstreamModelName string
	IsProbe           bool
}

func (r *RoutingRepo) UpsertChannelModel(ctx context.Context, in ChannelModelInput) error {
	if err := r.q.UpsertChannelModel(ctx, dbgen.UpsertChannelModelParams{
		ChannelID: in.ChannelID, ModelID: in.ModelID, UpstreamModelName: in.UpstreamModelName,
		Enabled: true, IsProbe: in.IsProbe,
	}); err != nil {
		return fmt.Errorf("upsert channel model: %w", err)
	}
	return nil
}

// ── 路由策略写入口（X9）──────────────────────────────────────────

type RoutingPolicyInput struct {
	Name          string
	ScopeKind     string // global | workspace | model
	ScopeID       string
	Strategy      string
	Weights       *gateway.RoutingWeights // 仅 strategy=custom 时使用
	FallbackDepth int32
	UpdatedBy     string
}

// UpsertRoutingPolicy 写入一条路由策略。
//
// **custom 权重的校验和必须为 1，写入时就拦住**——不要等到评分时才发现，
// 那时错误会表现为"排序看起来有点怪"，极难归因（UNIFIED §5.5，X9 判据）。
func (r *RoutingRepo) UpsertRoutingPolicy(ctx context.Context, in RoutingPolicyInput) error {
	var weightsJSON []byte
	if in.Strategy == "custom" {
		if in.Weights == nil {
			return fmt.Errorf("routing policy: strategy=custom requires an explicit weight vector")
		}
		issues := gateway.ValidateCustomWeights(*in.Weights)
		if gateway.HasBlockingWeightIssue(issues) {
			return fmt.Errorf("routing policy: invalid custom weights: %s", gateway.SummarizeWeightIssues(issues))
		}
		b, err := json.Marshal(in.Weights)
		if err != nil {
			return err
		}
		weightsJSON = b
	}

	var scopeID *string
	if in.ScopeID != "" {
		scopeID = &in.ScopeID
	}
	if _, err := r.q.UpsertRoutingPolicy(ctx, dbgen.UpsertRoutingPolicyParams{
		Name: in.Name, ScopeKind: in.ScopeKind, ScopeID: scopeID, Strategy: in.Strategy,
		Weights: weightsJSON, FallbackDepth: in.FallbackDepth, UpdatedBy: in.UpdatedBy,
	}); err != nil {
		return fmt.Errorf("upsert routing policy: %w", err)
	}
	return nil
}

// ── 探针退休（B4）──────────────────────────────────────────────

var _ gateway.HealthCheckRepository = (*RoutingRepo)(nil)

func (r *RoutingRepo) ChannelsDueForProbe(ctx context.Context) ([]gateway.DueChannel, error) {
	rows, err := r.q.ListChannelsDueForProbe(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels due for probe: %w", err)
	}
	out := make([]gateway.DueChannel, 0, len(rows))
	for _, row := range rows {
		out = append(out, gateway.DueChannel{
			ChannelID: row.ID, ConsecutiveFailures: row.ConsecutiveFailures,
			ProbeIntervalSeconds: row.ProbeIntervalSeconds,
		})
	}
	return out, nil
}

// ChannelDriverInfo 供健康检查循环取一个渠道的 Profile 与探测用的模型。
// upstreamModel 为空表示这个渠道没有标 is_probe 的模型，调用方应当跳过
// ——随便挑一个模型探测会得到"渠道健康但用户要的那个模型 404"这种
// 最具误导性的结果（UNIFIED §5.6-①）。
func (r *RoutingRepo) ChannelDriverInfo(ctx context.Context, channelID int64) (provider.Profile, string, string, error) {
	cand, _, err := r.GetChannel(ctx, channelID)
	if err != nil {
		return provider.Profile{}, "", "", err
	}
	_, upstreamModel, billingModel, err := r.GetProbeModel(ctx, channelID)
	if err != nil {
		return provider.Profile{}, "", "", err
	}
	return cand.Profile, upstreamModel, billingModel, nil
}

func (r *RoutingRepo) TouchProbe(ctx context.Context, channelID int64, nextIntervalSeconds int32) error {
	if err := r.q.TouchChannelProbe(ctx, dbgen.TouchChannelProbeParams{
		ID: channelID, ProbeIntervalSeconds: nextIntervalSeconds,
	}); err != nil {
		return fmt.Errorf("touch channel probe: %w", err)
	}
	return nil
}

// GetChannel 取一个渠道的完整信息（含解密后的 Profile），供 X10 的手工
// 连通性测试使用——测试要显式指定渠道，走这条查询而不是 ListCandidates。
func (r *RoutingRepo) GetChannel(ctx context.Context, channelID int64) (*gateway.RawCandidate, string, error) {
	row, err := r.q.GetChannelByID(ctx, channelID)
	if err != nil {
		return nil, "", fmt.Errorf("get channel: %w", err)
	}
	apiKey, err := r.decryptCredentials(row.CredentialsEnc)
	if err != nil {
		return nil, "", err
	}
	quirks, err := decodeQuirks(row.Quirks)
	if err != nil {
		return nil, "", err
	}
	baseURL := row.ProviderBaseUrl
	if row.BaseUrlOverride != nil && *row.BaseUrlOverride != "" {
		baseURL = *row.BaseUrlOverride
	}
	cand := &gateway.RawCandidate{
		ChannelID: row.ID, ChannelName: row.Name,
		Profile: provider.Profile{
			Name: row.Name, BaseURL: baseURL,
			AuthScheme: provider.AuthScheme(row.AuthScheme), AuthHeader: row.AuthHeader,
			APIKey: apiKey, Quirks: quirks,
		},
		Health: gateway.ChannelHealth{
			Alpha: row.HealthAlpha, Beta: row.HealthBeta,
			LatencyEMAMs: row.LatencyEmaMs, ConsecutiveFailures: row.ConsecutiveFailures,
			RateLimitedUntil: row.RateLimitedUntil,
		},
	}
	return cand, row.ProviderSlug, nil
}

// GetProbeModel 实现 X10 优先级第二档：该渠道标了 is_probe 的模型。
func (r *RoutingRepo) GetProbeModel(ctx context.Context, channelID int64) (modelID int64, upstreamModel, billingModel string, err error) {
	row, getErr := r.q.GetProbeChannelModel(ctx, channelID)
	if getErr != nil {
		if errors.Is(getErr, pgx.ErrNoRows) {
			return 0, "", "", nil
		}
		return 0, "", "", fmt.Errorf("get probe channel model: %w", getErr)
	}
	return row.ModelID, row.UpstreamModelName, row.BillingModel, nil
}
