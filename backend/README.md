# uMaaS backend

Go 实现的控制平面与推理数据平面。设计文档见本目录的六份 `.md`，
任务分解与迭代计划见 [`IMPLEMENTATION-PLAN.md`](./IMPLEMENTATION-PLAN.md)。

**当前进度：I0（骨架与纪律）、I1（身份）、I2（目录与价目）、I3（数据平面 MVP）已完成。**
业务端点从 I4（供给与路由）继续接入。

## 快速开始

```bash
make dev-up                       # 起 Postgres + Redis
export UMAAS_DATABASE__DSN="postgres://umaas:umaas@localhost:5432/umaas?sslmode=disable"
# 数据平面从 I3 起需要一条上游渠道配置（单渠道直连，I4 才有多渠道路由）：
export UMAAS_GATEWAY__CHANNEL__BASE_URL="https://api.openai.com"
export UMAAS_GATEWAY__CHANNEL__API_KEY="sk-..."
make migrate                      # 迁移（生产环境这是部署流水线的独立步骤）
./bin/umaas seed catalog           # 开发用种子目录数据（走 admin 写入路径，带全部校验）
make build && ./bin/umaas         # 两个平面一起跑（monolith 拓扑）
```

验证：

```bash
curl localhost:8080/healthz       # 控制平面，带信封
curl localhost:8080/readyz        # 依赖就绪状态
curl localhost:9090/metrics       # Prometheus 指标
curl -XPOST localhost:8081/v1/chat/completions \
  -d '{"model":"openai/gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}'
  # 数据平面，OpenAI 格式，无信封；model 用目录的 provider/slug 形态
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
| **展示价与结算价同源** | `internal/pricing`，唯一的 `pricing_effective_version()` | 两条查询路径迟早分叉，用户会先于我们发现价格不一致 |
| **流式首字节前不写任何字节** | `internal/gateway/completions.go` | 一旦提前写了状态码，中途失败就再也无法改成干净的错误响应 |
| **流式失败据实上报，绝不假装 `finish_reason: stop`** | `internal/protocol/openai` 的 `EventError` | 客户端会把截断的内容当成完整答案，比报错更有害 |
| **两段流式超时用同一个可重置定时器** | `internal/gateway/watchdog.go` | 只设首字节超时，上游中途卡住时会一直挂到客户端自己放弃 |

## 目录

```
cmd/umaas/                  主二进制，可同时或分别启用两个平面；含 admin/seed 子命令
cmd/umaas-migrate/          迁移工具（生产环境的独立部署步骤）
internal/config/            分层配置 + 启动期校验
internal/domain/            领域模型与错误语义（不认识 HTTP）
internal/store/             连接池、事务、迁移
internal/httpapi/           控制平面：信封、错误映射、中间件
internal/httpapi/catalog/   目录只读端点的 HTTP 层
internal/httpapi/admincatalog/ admin 的目录与价目写端点
internal/catalog/           目录组装（读侧）
internal/catalogadmin/      目录与价目写入侧（校验、状态机、审计）
internal/pricing/           价目解析与 nano 精度计算，展示/结算唯一入口
internal/ir/                协议无关的规范表示（Canonical IR）
internal/protocol/openai/   OpenAI Chat 协议 ↔ IR 转换（含流式）
internal/provider/          L4 传输层：ProviderProfile + 驱动接口
internal/provider/openaicompat/ 约 30 家 OpenAI 兼容上游共用的驱动
internal/gateway/           数据平面：OpenAI 兼容，无信封；单渠道直连（I3）
internal/requestlog/        request_logs 异步批量写入
internal/assembly/          拓扑装配层（monolith / split）
internal/observ/            日志、指标
internal/platform/          跨领域基础设施（request_id 等）
tools/depcheck/              依赖图与模块边界检查
db/migrations/               goose 迁移，embed 进二进制
db/seed/                     开发用种子数据（走 admin 写入路径）
deploy/                      compose 与反代样例
```
