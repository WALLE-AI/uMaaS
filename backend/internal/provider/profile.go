// Package provider 是 L4 传输层：把规范请求变成一次真实的 HTTP 调用
// （UNIFIED-PROVIDER-INTERFACE.md §4.4、§8）。
//
// I4 起，Profile 的来源是数据库里的 provider_profiles + channels 两张表
// （internal/store/postgres.RoutingRepo），而不是启动期配置——这正是
// P2 判据的落地：新增一个 OpenAI 兼容厂商是数据库里的几行，驱动接口
// 本身不需要因此改变形状。
package provider

// AuthScheme 是鉴权方式。约 30 家 OpenAI 兼容厂商的差异几乎全部
// 落在这里和 Quirks 上，而不是需要一份新代码（§4.4）。
type AuthScheme string

const (
	AuthBearer AuthScheme = "bearer" // Authorization: Bearer <key>
	AuthHeader AuthScheme = "header" // 自定义头，如 x-api-key
)

// Quirks 是与"标准 OpenAI 协议"的偏离项。
//
// 绝大多数厂商只需要设一两个字段——这正是"零代码接入"能成立的关键：
// 差异被收敛成几个布尔/字符串开关，而不是分支代码。
type Quirks struct {
	// NoStreamUsage 为 true 时不发送 stream_options.include_usage，
	// 且流式响应的 usage 一律按 estimated 处理（不等它，因为不会来）。
	NoStreamUsage bool
	// MaxTokensRequired 为 true 时，客户端未传 max_tokens 也要给上游填一个默认值，
	// 否则该厂商会直接拒绝请求。
	MaxTokensRequired bool
}

// Profile 描述一个上游的连接方式，对应一条 channels 行
// （合并了它所属 provider_profiles 行的协议族信息）。
type Profile struct {
	Name       string
	BaseURL    string // 如 "https://api.openai.com"
	AuthScheme AuthScheme
	AuthHeader string // AuthScheme=header 时使用，如 "x-api-key"
	APIKey     string
	Quirks     Quirks
}

// ChatPath 是 chat completions 的路径。I3 只支持这一个能力，
// 硬编码在这里；I4 引入多能力时会变成按 Capability 查表。
const ChatPath = "/v1/chat/completions"
