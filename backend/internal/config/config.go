// Package config 是分层配置的唯一入口：默认值 → 配置文件 → 环境变量。
//
// 只承载启动期配置。运行时可改的配置项走 settings 表（ARCHITECTURE.md §4.5），
// 两者边界必须划死——数据库连接串、监听端口、加密密钥永远不进 settings 表。
package config

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Topology 决定服务如何装配。同一份代码支持两种拓扑（SERVICE-DECOMPOSITION.md §4）。
type Topology string

const (
	// TopologyMonolith 进程内直调。本地开发与拆分尚未触发时的生产形态。
	TopologyMonolith Topology = "monolith"
	// TopologySplit 跨进程 gRPC。S2 之后启用。
	TopologySplit Topology = "split"
)

// Config 是全部启动期配置。
type Config struct {
	Env      string   `koanf:"env"` // dev | staging | prod
	Topology Topology `koanf:"topology"`

	ControlPlane PlaneConfig `koanf:"control_plane"`
	DataPlane    PlaneConfig `koanf:"data_plane"`

	Database DatabaseConfig `koanf:"database"`
	Security SecurityConfig `koanf:"security"`
	Redis    RedisConfig    `koanf:"redis"`
	Observ   ObservConfig   `koanf:"observability"`
	Log      LogConfig      `koanf:"log"`
	Gateway  GatewayConfig  `koanf:"gateway"`
}

// GatewayConfig 是数据平面 I3 阶段的配置。
//
// **这是临时状态**：I4 的 P2/P3 会把 Channel 的来源从"一份启动期配置"
// 换成数据库里的 channels 表 + 权重/健康检查选择逻辑。现在只有单渠道
// 直连，配置一条足够，且这条配置在拆分成 channels 表时字段可以直接对应，
// 不用推倒重来。
type GatewayConfig struct {
	Channel ChannelConfig `koanf:"channel"`
	Stream  StreamConfig  `koanf:"stream"`
}

// ChannelConfig 是**引导用**的一条渠道定义。
//
// I4 起，路由渠道的真相源是数据库（provider_profiles/channels，
// internal/store/postgres.RoutingRepo）——新增一个 OpenAI 兼容厂商是
// 数据库里的几行，不需要重启进程改配置。这个结构体只在 `umaas seed`
// 引导本地开发环境的第一条渠道时使用，服务启动本身不再依赖它。
type ChannelConfig struct {
	Name       string `koanf:"name"`
	BaseURL    string `koanf:"base_url"`
	APIKey     string `koanf:"api_key"`
	AuthScheme string `koanf:"auth_scheme"` // bearer | header
	AuthHeader string `koanf:"auth_header"` // AuthScheme=header 时使用
	// Quirks（UNIFIED-PROVIDER-INTERFACE.md §4.4）：与标准协议的偏离项，
	// 现在只暴露两个最常见的，其余等接入第二家上游时再加。
	NoStreamUsage     bool `koanf:"no_stream_usage"`
	MaxTokensRequired bool `koanf:"max_tokens_required"`
}

// StreamConfig 是两段流式超时（UNIFIED-PROVIDER-INTERFACE.md §7）。
//
// 只设首字节超时不够：流已开始、上游中途卡住不再吐字节时，首字节超时
// 早已失效。两者缺一都会在生产上表现为"某类请求偶尔挂住"，且现象
// 与业务代码毫无关系，排查方向天然错误。
type StreamConfig struct {
	// FirstByteTimeout 覆盖"发出请求 → 首个 chunk"。仍在安全窗口内，
	// I4 引入多渠道后这个超时触发时可以换渠道重试。
	FirstByteTimeout time.Duration `koanf:"first_byte_timeout"`
	// StallTimeout 覆盖"相邻两个 chunk 之间"。触发时已经吐给客户端
	// 部分内容，只能中止并如实上报，不能重试。
	StallTimeout time.Duration `koanf:"stall_timeout"`
}

// PlaneConfig 控制单个平面。两个平面独立启停，是 ARCHITECTURE.md §1 的直接落地。
type PlaneConfig struct {
	Enabled bool   `koanf:"enabled"`
	Addr    string `koanf:"addr"`

	ReadHeaderTimeout time.Duration `koanf:"read_header_timeout"`
	// WriteTimeout 对数据平面必须是 0：SSE 流可以持续几分钟，
	// 任何整体写超时都会在正常的长响应中途砍断连接（UNIFIED-PROVIDER-INTERFACE.md §7）。
	WriteTimeout    time.Duration `koanf:"write_timeout"`
	IdleTimeout     time.Duration `koanf:"idle_timeout"`
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout"`
}

type DatabaseConfig struct {
	DSN             string        `koanf:"dsn"`
	MaxConns        int32         `koanf:"max_conns"`
	MinConns        int32         `koanf:"min_conns"`
	MaxConnLifetime time.Duration `koanf:"max_conn_lifetime"`
	// PgBouncerMode 为 true 时关闭 prepared statement 缓存。
	// Supabase 默认给的是 PgBouncer transaction 模式地址（6543 端口），
	// 该模式不支持 prepared statement——这是接 Supabase 最常见的坑（ARCHITECTURE.md §2.4）。
	PgBouncerMode bool `koanf:"pgbouncer_mode"`
	// AutoMigrate 只允许在 dev 打开。生产环境迁移是部署流水线的独立步骤，
	// 多副本同时启动会竞争迁移锁，且一次失败的迁移会让整个服务起不来（ARCHITECTURE.md §8）。
	AutoMigrate bool `koanf:"auto_migrate"`
}

// SecurityConfig 承载加密与 cookie 策略。
//
// 密钥只能来自环境变量或 KMS，永远不进 settings 表——
// 边界不划死，迟早有人把它做成一个能在网页上编辑的输入框（§4.5）。
type SecurityConfig struct {
	// EncryptionKey 是 32 字节密钥的 hex 或 base64 编码，
	// 用于 TOTP 密钥与上游渠道凭证（AES-GCM）。
	EncryptionKey string `koanf:"encryption_key"`
	// SecureCookies 控制 Set-Cookie 的 Secure 属性。
	// 生产环境强制为 true，见 Validate。
	SecureCookies bool `koanf:"secure_cookies"`
	// CORSAllowedOrigins 是允许带凭证访问的来源。不支持通配符：
	// 带 cookie 的请求下 Access-Control-Allow-Origin 不允许是 *。
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins"`
}

type RedisConfig struct {
	Addr     string `koanf:"addr"`
	Password string `koanf:"password"`
	DB       int    `koanf:"db"`
}

type ObservConfig struct {
	ServiceName    string `koanf:"service_name"`
	MetricsEnabled bool   `koanf:"metrics_enabled"`
	MetricsAddr    string `koanf:"metrics_addr"`
}

type LogConfig struct {
	Level  string `koanf:"level"`  // debug | info | warn | error
	Format string `koanf:"format"` // json | text
}

func defaults() *Config {
	return &Config{
		Env:      "dev",
		Topology: TopologyMonolith,
		ControlPlane: PlaneConfig{
			Enabled:           true,
			Addr:              ":8080",
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
			ShutdownTimeout:   20 * time.Second,
		},
		DataPlane: PlaneConfig{
			Enabled:           true,
			Addr:              ":8081",
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      0, // 见 PlaneConfig.WriteTimeout 的注释
			IdleTimeout:       120 * time.Second,
			ShutdownTimeout:   60 * time.Second,
		},
		Database: DatabaseConfig{
			MaxConns:        20,
			MinConns:        2,
			MaxConnLifetime: time.Hour,
		},
		Observ: ObservConfig{
			ServiceName:    "umaas",
			MetricsEnabled: true,
			MetricsAddr:    ":9090",
		},
		Security: SecurityConfig{
			SecureCookies:      false, // 本地开发走 http
			CORSAllowedOrigins: []string{"http://localhost:5173", "http://localhost:5174"},
		},
		Log: LogConfig{Level: "info", Format: "json"},
		Gateway: GatewayConfig{
			Stream: StreamConfig{
				FirstByteTimeout: 10 * time.Second,
				StallTimeout:     30 * time.Second,
			},
		},
	}
}

// Load 按 默认值 → 文件 → 环境变量 的顺序合并，最后校验。
// path 为空则跳过文件层。环境变量前缀 UMAAS_，双下划线表示层级：
// UMAAS_DATABASE__DSN → database.dsn
func Load(path string) (*Config, error) {
	k := koanf.New(".")
	cfg := defaults()

	if err := k.Load(structsProvider(cfg), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load config file %q: %w", path, err)
		}
	}
	envProvider := env.Provider("UMAAS_", ".", func(key string) string {
		key = strings.ToLower(strings.TrimPrefix(key, "UMAAS_"))
		return strings.ReplaceAll(key, "__", ".")
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	out := &Config{}
	if err := k.UnmarshalWithConf("", out, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// Validate 在启动期拒绝不合法的组合。宁可起不来，也不要带着错配置跑。
func (c *Config) Validate() error {
	switch c.Env {
	case "dev", "staging", "prod":
	default:
		return fmt.Errorf("config: env must be dev|staging|prod, got %q", c.Env)
	}
	switch c.Topology {
	case TopologyMonolith, TopologySplit:
	default:
		return fmt.Errorf("config: topology must be monolith|split, got %q", c.Topology)
	}
	if !c.ControlPlane.Enabled && !c.DataPlane.Enabled {
		return fmt.Errorf("config: at least one plane must be enabled")
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("config: database.dsn is required")
	}
	if c.Database.MaxConns < c.Database.MinConns {
		return fmt.Errorf("config: database.max_conns (%d) < min_conns (%d)",
			c.Database.MaxConns, c.Database.MinConns)
	}
	// 生产环境禁止启动期自动迁移。这一条在配置层拦，而不是靠部署脚本自觉。
	if c.Env == "prod" && c.Database.AutoMigrate {
		return fmt.Errorf("config: database.auto_migrate must be false in prod; " +
			"run `umaas-migrate up` as a separate deploy step")
	}
	// 数据平面的 WriteTimeout 必须为 0，否则长流会被中途砍断。
	if c.DataPlane.Enabled && c.DataPlane.WriteTimeout != 0 {
		return fmt.Errorf("config: data_plane.write_timeout must be 0 (SSE streams run for minutes); "+
			"got %s", c.DataPlane.WriteTimeout)
	}
	// 生产环境必须有加密密钥：没有它，管理员的 TOTP 密钥与上游凭证无处安放，
	// 而这个缺失只会在第一次创建管理员时才暴露。
	if c.Env == "prod" {
		if c.Security.EncryptionKey == "" {
			return fmt.Errorf("config: security.encryption_key is required in prod")
		}
		if !c.Security.SecureCookies {
			return fmt.Errorf("config: security.secure_cookies must be true in prod " +
				"(the contract requires HttpOnly; Secure; SameSite=Lax)")
		}
	}
	if c.Security.EncryptionKey != "" {
		if _, err := c.EncryptionKeyBytes(); err != nil {
			return fmt.Errorf("config: security.encryption_key: %w", err)
		}
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: log.level must be debug|info|warn|error, got %q", c.Log.Level)
	}
	return nil
}

// ValidateGateway 拒绝不能启动数据平面的网关配置。
//
// **不放进 Validate()**：`umaas-migrate`、`umaas admin create`、`umaas seed`
// 都要走 config.Load，但它们不启动数据平面（config.go 早先在这里踩过
// 一次坑：把渠道要求塞进 Validate() 导致迁移工具直接拒绝启动）。
// 调用方只在真的要装配数据平面路由前调用这个方法。
//
// **不检查 gateway.channel.base_url**：I4 起渠道来自数据库，进程启动时
// 允许一条渠道都没有——那是"这个模型暂无供给"的运行时状态（B3 的
// `no_channels_available`），不是不能起服务的理由。
func (c *Config) ValidateGateway() error {
	if c.Gateway.Stream.FirstByteTimeout <= 0 || c.Gateway.Stream.StallTimeout <= 0 {
		return fmt.Errorf("config: gateway.stream.first_byte_timeout and stall_timeout must be positive " +
			"(UNIFIED-PROVIDER-INTERFACE.md §7); a zero value would hang forever instead of failing loudly")
	}
	return nil
}

// EncryptionKeyBytes 解码密钥。接受 hex 或 base64，都必须解出 32 字节。
func (c *Config) EncryptionKeyBytes() ([]byte, error) {
	raw := strings.TrimSpace(c.Security.EncryptionKey)
	if raw == "" {
		return nil, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, fmt.Errorf("must be 32 bytes encoded as hex or base64")
}
