// Package admincatalog 是 admin 的目录与价目写端点（M1a）。
//
// 它与面向 web 的 internal/httpapi/catalog 分开挂载：两者的鉴权链、可见的
// 模型状态、以及"读到的是不是解析后的有效价"都不同。
// I6 的 B6b 会在这里继续补齐目录组的其余端点（upstream-diff / channels / routing）。
package admincatalog

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/WALLE-AI/uMaaS/backend/internal/catalogadmin"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	authapi "github.com/WALLE-AI/uMaaS/backend/internal/httpapi/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/httpapi/response"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

type Handler struct {
	svc *catalogadmin.Service
}

func New(svc *catalogadmin.Service) *Handler { return &Handler{svc: svc} }

// Routes 挂在已经过 RequireAdmin 的子路由下。
//
// 写操作再叠一层 RequireAdminWrite：viewer 角色只读这条规则放在领域层
// （AdminRole.CanWrite），这里只是把它接到路由上——每个 handler 各写一遍
// 迟早会漏掉一个端点。
func (h *Handler) Routes(r chi.Router) {
	r.Get("/models", h.listModels)
	r.Get("/models/{id}/pricing", h.priceHistory)

	r.Group(func(r chi.Router) {
		r.Use(authapi.RequireAdminWrite)
		r.Post("/models", h.upsertModel)
		r.Post("/models/{id}/pricing/preview", h.previewPrice)
		r.Post("/models/{id}/pricing", h.publishPrice)
		r.Post("/models/{id}/publish", h.publish)
		r.Post("/models/{id}/delist", h.delist)
	})
}

// ── 载荷 ────────────────────────────────────────────────────────

// adminModelPayload 与 admin 前端的 CatalogModel 对应（计价 附 1.4）。
//
// **价格是一个完整的 pricing 对象，不是 inputPrice/outputPrice 两个数**：
// web 侧新增的 cache_read / cache_write / tiers 字段，若 admin 没有录入口
// 就永远是 null——加了等于没加，`App.tsx` 那个 `input × 0.1` 的猜测也就删不掉。
type adminModelPayload struct {
	ID            string          `json:"id"`
	ModelID       string          `json:"model_id"`
	Name          string          `json:"name"`
	Vendor        string          `json:"vendor"`
	Hosting       string          `json:"hosting"`
	Status        string          `json:"status"`
	CanaryPercent *int32          `json:"canary_percent,omitempty"`
	BillingModel  string          `json:"billing_model"`
	ContextLength *int32          `json:"context_length"`
	Capabilities  []string        `json:"capabilities"`
	CallsLast7d   int64           `json:"calls_last_7d"`
	Pricing       *pricingPayload `json:"pricing"`
	ReleasedAt    string          `json:"released_at"`
}

// pricingPayload 在 admin 侧**同时给出 USD 与 nano**。
//
// USD 是给人看的，nano 是真相。只给 USD 会让前端在回填表单时做一次
// 浮点往返，而 0.15 这种数在二进制浮点里不精确——往返一次就可能变成
// 0.149999999，再保存回来就是一个新的价目版本。
type pricingPayload struct {
	PriceVersionID   int64    `json:"price_version_id"`
	Currency         string   `json:"currency"`
	EffectiveFrom    string   `json:"effective_from"`
	Source           string   `json:"source"`
	AppliedBy        string   `json:"applied_by"`
	Note             string   `json:"note"`
	Rates            any      `json:"rates"`
	InputPerMillion  *float64 `json:"input_per_million"`
	OutputPerMillion *float64 `json:"output_per_million"`
}

func toPricingPayload(v *pricing.Version) *pricingPayload {
	if v == nil {
		return nil
	}
	p := &pricingPayload{
		PriceVersionID: v.ID, Currency: v.Currency,
		EffectiveFrom: v.EffectiveFrom.UTC().Format(time.RFC3339),
		Source:        v.Source, AppliedBy: v.AppliedBy, Note: v.Note,
		Rates: json.RawMessage(v.Rates.Raw()),
	}
	if nano, ok := v.Rates.PerMillionNano(pricing.UnitInput); ok {
		usd := pricing.PerMillionUSD(nano)
		p.InputPerMillion = &usd
	}
	if nano, ok := v.Rates.PerMillionNano(pricing.UnitOutput); ok {
		usd := pricing.PerMillionUSD(nano)
		p.OutputPerMillion = &usd
	}
	return p
}

func toAdminModel(m catalogadmin.Model) adminModelPayload {
	return adminModelPayload{
		ID: strconv.FormatInt(m.ID, 10), ModelID: m.PublicID(),
		Name: m.DisplayName, Vendor: m.ProviderName, Hosting: m.Hosting,
		Status: m.Status, CanaryPercent: m.CanaryPercent,
		BillingModel: m.BillingModel, ContextLength: m.ContextLength,
		Capabilities: m.Capabilities, CallsLast7d: m.CallsLast7d,
		Pricing:    toPricingPayload(m.Price),
		ReleasedAt: m.ReleasedAt.UTC().Format(time.RFC3339),
	}
}

// ── 端点 ────────────────────────────────────────────────────────

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.svc.List(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := make([]adminModelPayload, 0, len(models))
	for _, m := range models {
		out = append(out, toAdminModel(m))
	}
	response.OK(w, r, out)
}

type upsertModelRequest struct {
	Provider struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		LogoURL     string `json:"logo_url"`
		Description string `json:"description"`
	} `json:"provider"`
	Slug                string   `json:"slug"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	LogoURL             string   `json:"logo_url"`
	Modalities          []string `json:"modalities"`
	Capabilities        []string `json:"capabilities"`
	ContextLength       *int32   `json:"context_length"`
	MaxOutputTokens     *int32   `json:"max_output_tokens"`
	Architecture        *string  `json:"architecture"`
	InputFormats        []string `json:"input_formats"`
	OutputFormats       []string `json:"output_formats"`
	SupportedParameters []string `json:"supported_parameters"`
	ZeroDataRetention   bool     `json:"zero_data_retention"`
	OpenWeights         bool     `json:"open_weights"`
	Hosting             string   `json:"hosting"`
	BillingModel        string   `json:"billing_model"`
	ReleasedAt          *string  `json:"released_at"`
}

func (h *Handler) upsertModel(w http.ResponseWriter, r *http.Request) {
	var req upsertModelRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	in := catalogadmin.ModelInput{
		Slug: req.Slug, DisplayName: req.Name, Description: req.Description,
		LogoURL: req.LogoURL, Modalities: req.Modalities, Capabilities: req.Capabilities,
		ContextLength: req.ContextLength, MaxOutputTokens: req.MaxOutputTokens,
		Architecture: req.Architecture, InputFormats: req.InputFormats,
		OutputFormats: req.OutputFormats, SupportedParameters: req.SupportedParameters,
		ZeroDataRetention: req.ZeroDataRetention, OpenWeights: req.OpenWeights,
		Hosting: req.Hosting, BillingModel: req.BillingModel,
	}
	if req.ReleasedAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ReleasedAt)
		if err != nil {
			response.Error(w, r, domain.Validation("invalid_released_at",
				"released_at must be an RFC3339 timestamp", "released_at"))
			return
		}
		in.ReleasedAt = t
	}
	if len(in.Modalities) == 0 {
		in.Modalities = []string{"text"}
	}

	m, err := h.svc.UpsertModel(r.Context(), actorFrom(r), catalogadmin.ProviderInput{
		Slug: req.Provider.Slug, Name: req.Provider.Name, Kind: req.Provider.Kind,
		LogoURL: req.Provider.LogoURL, Description: req.Provider.Description,
	}, in)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, toAdminModel(*m), nil)
}

type pricingRequest struct {
	ScopeKind     string          `json:"scope_kind"`
	ScopeID       string          `json:"scope_id"`
	Currency      string          `json:"currency"`
	Rates         json.RawMessage `json:"rates"`
	EffectiveFrom *string         `json:"effective_from"`
	Note          string          `json:"note"`
	Source        string          `json:"source"`
	Confirm       bool            `json:"confirm"`
}

func (h *Handler) proposal(r *http.Request, req pricingRequest) (catalogadmin.PriceProposal, error) {
	id, err := pathID(r)
	if err != nil {
		return catalogadmin.PriceProposal{}, err
	}
	p := catalogadmin.PriceProposal{
		ModelID: id, ScopeKind: req.ScopeKind, ScopeID: req.ScopeID,
		Currency: req.Currency, Rates: req.Rates, Note: req.Note,
		Source: req.Source, Confirm: req.Confirm,
	}
	if len(p.Rates) == 0 {
		return p, domain.Validation("rates_required", "rates is required", "rates")
	}
	if req.EffectiveFrom != nil {
		t, err := time.Parse(time.RFC3339, *req.EffectiveFrom)
		if err != nil {
			return p, domain.Validation("invalid_effective_from",
				"effective_from must be an RFC3339 timestamp", "effective_from")
		}
		p.EffectiveFrom = t
	}
	return p, nil
}

// previewPrice 是提交前的体检：校验问题 + 毛利预览，不写库。
//
// 它单独成一个端点而不是"保存时才告诉你"，是因为 §3.6-④ 的校验里
// 有一半是**警告**——警告要在人还在编辑框里时出现才有意义。
func (h *Handler) previewPrice(w http.ResponseWriter, r *http.Request) {
	var req pricingRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	p, err := h.proposal(r, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	preview, _, _, err := h.svc.PreviewPrice(r.Context(), p)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, preview)
}

// publishPrice 写入新版本。
//
// 校验不通过时返回 422，**并把体检结果一起带回去**：只回一句
// "validation failed" 会逼着前端再调一次 preview。
func (h *Handler) publishPrice(w http.ResponseWriter, r *http.Request) {
	var req pricingRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	p, err := h.proposal(r, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	version, preview, err := h.svc.PublishPrice(r.Context(), actorFrom(r), p)
	if err != nil {
		if preview != nil {
			writeRejection(w, r, err, preview)
			return
		}
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, map[string]any{
		"price_version_id": version.ID,
		"billing_model":    version.BillingModel,
		"scope_kind":       version.ScopeKind,
		"effective_from":   version.EffectiveFrom.UTC().Format(time.RFC3339),
		"preview":          preview,
	}, nil)
}

func (h *Handler) priceHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	limit := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			response.Error(w, r, domain.Validation("invalid_limit", "limit must be a positive integer", "limit"))
			return
		}
		limit = int32(n)
	}
	versions, err := h.svc.PriceHistory(r.Context(), id, limit)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := make([]*pricingPayload, 0, len(versions))
	for _, v := range versions {
		out = append(out, toPricingPayload(v))
	}
	response.OK(w, r, out)
}

type publishRequest struct {
	Status        string `json:"status"`
	CanaryPercent *int32 `json:"canary_percent"`
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req publishRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	m, err := h.svc.Publish(r.Context(), actorFrom(r), id, req.Status, req.CanaryPercent)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, toAdminModel(*m))
}

func (h *Handler) delist(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	m, err := h.svc.Delist(r.Context(), actorFrom(r), id, req.Reason)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, toAdminModel(*m))
}

// ── 辅助 ────────────────────────────────────────────────────────

// writeRejection 在 422 的错误体里附上体检结果。
//
// 信封的 ErrorBody 只有 code/message/field 三个字段，因此这里用
// 一个 200 之外的状态码 + 自定义体是行不通的——保持信封形状，
// 把细节放进 details。
func writeRejection(w http.ResponseWriter, r *http.Request, err error, preview *catalogadmin.PreviewResult) {
	response.ErrorWithDetails(w, r, err, map[string]any{
		"issues": preview.Issues,
		"margin": preview.Margin,
	})
}

func actorFrom(r *http.Request) catalogadmin.Actor {
	admin, ok := authapi.AdminFromContext(r.Context())
	if !ok {
		return catalogadmin.Actor{Label: "unknown", RequestID: platform.RequestID(r.Context())}
	}
	return catalogadmin.Actor{
		ID: admin.Account.ID, Label: admin.Account.Email,
		IP: clientIP(r), RequestID: platform.RequestID(r.Context()),
	}
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, domain.Validation("invalid_id", "model id must be a positive integer", "id")
	}
	return id, nil
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.Validation("invalid_body", "request body is not valid JSON", "")
	}
	return nil
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, _ := splitHostPort(r.RemoteAddr)
	return host
}

func splitHostPort(addr string) (string, string, bool) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], true
		}
	}
	return addr, "", false
}
