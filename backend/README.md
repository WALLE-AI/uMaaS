# uMaaS backend

Go 实现的控制平面与推理数据平面。设计文档见本目录的六份 `.md`，
任务分解与迭代计划见 [`IMPLEMENTATION-PLAN.md`](./IMPLEMENTATION-PLAN.md)。

**当前进度：I0（骨架与纪律）已完成。** 业务端点从 I1 开始逐个接入。

## 快速开始

```bash
make dev-up                       # 起 Postgres + Redis
export UMAAS_DATABASE__DSN="postgres://umaas:umaas@localhost:5432/umaas?sslmode=disable"
make migrate                      # 迁移（生产环境这是部署流水线的独立步骤）
make build && ./bin/umaas         # 两个平面一起跑（monolith 拓扑）
```

验证：

```bash
curl localhost:8080/healthz       # 控制平面，带信封
curl localhost:8080/readyz        # 依赖就绪状态
curl localhost:9090/metrics       # Prometheus 指标
curl -XPOST localhost:8081/v1/chat/completions   # 数据平面，OpenAI 格式，无信封
```

## 常用命令

| 命令 | 说明 |
|---|---|
| `make test` | 单元测试，不碰数据库 |
| `make test-integration` | 集成测试，跑在真实 Postgres 上 |
| `make check-deps` | 断言模块依赖图是 DAG，并检查平面隔离 |
| `make smoke` | monolith 冒烟测试（每个迭代的 DoD） |
| `make ci` | CI 跑的全部检查 |

集成测试默认用 testcontainers 起容器；已有现成的库时设 `UMAAS_TEST_DSN` 直接用它。

## 已经落地的几条硬约束

这些不是风格偏好，每条都对应一个具体的故障模式：

| 约束 | 落地位置 | 不这么做会怎样 |
|---|---|---|
| **数据平面不包信封** | `internal/gateway`，`tools/depcheck` 强制 | OpenAI SDK 解析失败，错误信息本身也丢了 |
| **数据平面 `write_timeout` 必须为 0** | `config.Validate` | 正常的长流会在中途被砍断 |
| **生产环境禁止启动期自动迁移** | `config.Validate` | 多副本竞争迁移锁；一次失败的迁移让整个服务起不来 |
| **`/healthz` 不查数据库** | `internal/httpapi/router.go` | 依赖故障时编排系统会反复重启健康的进程 |
| **反代必须关响应缓冲** | `deploy/nginx.conf.sample` | 应用层做对了，流式照样退化成"慢的非流式" |
| **要求事务的服务不得远程化** | `internal/assembly` | 切 split 拓扑后事务语义静默降级，症状是偶尔超额扣费 |
| **模块依赖图必须是 DAG** | `tools/depcheck`，进 CI | 依赖成环总是悄悄形成的，等想拆服务时已经缠死 |
| **测试用真实 Postgres，不用 SQLite** | `internal/store/testsupport` | 方言相关的 bug 全被排除在覆盖之外，测试全绿而生产炸掉 |

## 目录

```
cmd/umaas/            主二进制，可同时或分别启用两个平面
cmd/umaas-migrate/    迁移工具（生产环境的独立部署步骤）
internal/config/      分层配置 + 启动期校验
internal/domain/      领域模型与错误语义（不认识 HTTP）
internal/store/       连接池、事务、迁移
internal/httpapi/     控制平面：信封、错误映射、中间件
internal/gateway/     数据平面：OpenAI 兼容，无信封
internal/assembly/    拓扑装配层（monolith / split）
internal/observ/      日志、指标
internal/platform/    跨领域基础设施（request_id 等）
tools/depcheck/       依赖图与模块边界检查
db/migrations/        goose 迁移，embed 进二进制
deploy/               compose 与反代样例
```
