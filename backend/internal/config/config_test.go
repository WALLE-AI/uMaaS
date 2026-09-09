package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("UMAAS_DATABASE__DSN", "postgres://localhost:5432/umaas")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "dev", cfg.Env)
	assert.Equal(t, TopologyMonolith, cfg.Topology)
	assert.Equal(t, ":8080", cfg.ControlPlane.Addr)
	assert.Equal(t, ":8081", cfg.DataPlane.Addr)
	assert.Equal(t, 10*time.Second, cfg.ControlPlane.ReadHeaderTimeout)
	// 数据平面的写超时必须是 0，否则长流会被砍断。
	assert.Zero(t, cfg.DataPlane.WriteTimeout)
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("UMAAS_DATABASE__DSN", "postgres://example/db")
	t.Setenv("UMAAS_ENV", "staging")
	t.Setenv("UMAAS_CONTROL_PLANE__ADDR", ":9999")
	t.Setenv("UMAAS_LOG__LEVEL", "debug")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "staging", cfg.Env)
	assert.Equal(t, ":9999", cfg.ControlPlane.Addr)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "postgres://example/db", cfg.Database.DSN)
}

// 启动期校验是这一层的主要价值：宁可起不来，也不要带着错配置跑。
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "missing dsn",
			mutate:  func(c *Config) { c.Database.DSN = "" },
			wantErr: "database.dsn is required",
		},
		{
			name:    "unknown env",
			mutate:  func(c *Config) { c.Env = "production" },
			wantErr: "env must be dev|staging|prod",
		},
		{
			name:    "unknown topology",
			mutate:  func(c *Config) { c.Topology = "cluster" },
			wantErr: "topology must be monolith|split",
		},
		{
			name: "no plane enabled",
			mutate: func(c *Config) {
				c.ControlPlane.Enabled = false
				c.DataPlane.Enabled = false
			},
			wantErr: "at least one plane must be enabled",
		},
		{
			// 生产环境启动期自动迁移会让多副本竞争迁移锁（ARCHITECTURE.md §8）。
			name: "auto migrate in prod",
			mutate: func(c *Config) {
				c.Env = "prod"
				c.Database.AutoMigrate = true
			},
			wantErr: "auto_migrate must be false in prod",
		},
		{
			// SSE 流会跑几分钟，整体写超时会在正常长响应中途砍断连接。
			name:    "data plane write timeout set",
			mutate:  func(c *Config) { c.DataPlane.WriteTimeout = 30 * time.Second },
			wantErr: "data_plane.write_timeout must be 0",
		},
		{
			name: "max conns below min",
			mutate: func(c *Config) {
				c.Database.MaxConns = 1
				c.Database.MinConns = 5
			},
			wantErr: "max_conns",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			cfg.Database.DSN = "postgres://localhost/umaas"
			tc.mutate(cfg)

			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateAcceptsGoodConfig(t *testing.T) {
	cfg := defaults()
	cfg.Database.DSN = "postgres://localhost/umaas"
	require.NoError(t, cfg.Validate())
}

func TestNormalizeKey(t *testing.T) {
	assert.Equal(t, "database.dsn", NormalizeKey("UMAAS_DATABASE__DSN"))
	assert.Equal(t, "control_plane.addr", NormalizeKey("UMAAS_CONTROL_PLANE__ADDR"))
}
