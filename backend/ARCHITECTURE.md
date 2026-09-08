# uMaaS 后端技术架构设计方案

> 状态：待评审 · 2026-09-06
> 语言：Go · 数据库：**PostgreSQL / Supabase（唯一生产库）**
> 契约来源：`frontend/contracts/openapi.yaml`（面向 web 的 21 个端点）+ `frontend/admin/src/api/services.ts`（管理端 34 个端点，见 §5.5）+ OpenAI 兼容推理接口
> **控制平面实际是 55 个端点**，管理端比面向用户的门户还大——分期计划里的 B6 要按这个规模估工期
> 服务对象：`frontend/web`（开发者门户与自助控制台）、`frontend/admin`（平台管理控制台）

## 文档关系

本目录有六份设计文档，职责边界如下。**同一个决策只在一处详述，另一处引用**——重复维护必然漂移。

| | `ARCHITECTURE.md`（本文） | `UNIFIED-PROVIDER-INTERFACE.md` | `SERVICE-DECOMPOSITION.md` |
|---|---|---|---|
| 定位 | **全局约束**：技术栈、数据库、部署形态、平面划分 | 本文 §6「供给侧如何统一」的展开 | 本文 §8「部署形态」的展开 |
| 真相源 | 身份/计费/私有化部署的数据模型、控制平面实现、双数据库约束、分期计划 | 协议转换、适配器抽象、模型注册与路由索引、渠道评分、流式超时参数 | 服务边界、服务间通信、部署拓扑、拆分触发条件 |
| 依赖方向 | 定义其余三份必须遵守的边界（sqlc、单一 Postgres、Redis 为必需组件） | 在边界内展开，结论**反向修正**本文局部 | 与本文约束曾有冲突，见该文 §1 |

第四份 [`SELF-HOSTED-GPU-OPERATIONS.md`](./SELF-HOSTED-GPU-OPERATIONS.md) 是 `SERVICE-DECOMPOSITION.md` §3-④ Infra Agent 的展开，**是混合供给路由、GPU 容量管理、自建成本核算的真相源**。它依赖前两份定义的评分引擎与数据模型。

第六份 [`IMPLEMENTATION-PLAN.md`](./IMPLEMENTATION-PLAN.md) 是本文 §9「分期实施」的展开，**是任务分解、依赖顺序、迭代边界与交付判据的真相源**。它不产生新的技术决策，只把五条子序列（B/P/M/G/S）排到一条时间线上并裁决冲突。

第五份 [`BILLING-AND-PRICING.md`](./BILLING-AND-PRICING.md) 是本文 §4.3「用量与计费」与 §6.3「计量与计费」的展开，**是价目模型与版本化、计费口径、预扣—结算—释放、余额与信用额度、成本归集与毛利、计费对账的真相源**。它修正了本文 §4.1/§4.3 的两处字段设计（`balance_usd`、`cost_usd_micro`），改动请改那一处。

因此这些文档是**双向引用但非循环**：本文定边界，其余四份在边界内深入，深入的结果再回填本文。凡标注"真相源"的地方，改动只改那一处。

---

## 已定决策速查

截至 2026-09-08 已确认的决策，实施时以此为准。每条给出出处，细节看对应章节。

| # | 决策 | 出处 |
|---|---|---|
| 1 | 语言 **Go**；HTTP 框架 **Chi**（需 `net/http` 兼容以支持 SSE 转发） | 本文 §2.1 |
| 2 | 数据访问 **sqlc + pgx**，不用 ORM | 本文 §2.2/2.3 |
| 3 | **PostgreSQL / Supabase 为唯一生产库**；**放弃 SQLite，连测试也不用**（测试走 testcontainers） | 本文 §7 |
| 4 | Supabase **直连 Postgres**，不走 PostgREST / RLS | 本文 §2.4 |
| 5 | **Redis 为必需组件**，但只作原子裁决层，真相源仍在 Postgres | 本文 §2.5 |
| 6 | 部署形态：**云服务**，不做私有化单机交付 | 本文 §8 |
| 7 | 任务队列用 **river**（Postgres `SKIP LOCKED`），不引 MQ | 本文 §2.5 |
| 8 | **控制平面与推理数据平面分离**（独立 router / 中间件 / 连接池） | 本文 §1 |
| 9 | 架构走**模块化单体 + 可拆分缝**；四个服务边界中**只有 Gateway 是"不拆是错的"** | 拆分方案 §3 |
| 10 | **计费不跨服务边界**（否则要分布式事务或接受坏账） | 拆分方案 §1 |
| 11 | 同一份代码支持 `monolith` / `split` 两种装配，拆分是配置决策 | 拆分方案 §4 |
| 12 | 供给侧四层解耦；**约 30 家 OpenAI 兼容厂商零代码接入**（配置即可） | 统一接口 §3/§4.4 |
| 13 | 协议转换用**独立 IR + 高频路径直转**，并**公开质量分级与损失** | 统一接口 §4.1/4.2 |
| 14 | 渠道选择用**凸组合评分 + 乘性护栏 + Thompson 采样**，非 priority+weight | 统一接口 §5.5 |
| 15 | **运营自有 GPU 集群**；Infra Agent 为必需服务 | 自建 GPU §1 |
| 16 | 编排底座 **裸机 + Docker**，不用 K8s；compose 只管平台服务，推理实例走 Docker API | 自建 GPU §11.1/11.2 |
| 17 | 推理引擎 **首版只上 vLLM** | 自建 GPU §7 |
| 18 | GPU **自有机房 + 云上实例都有** → 供给分三层，成本策略各异 | 自建 GPU §1/§3 |
| 19 | 机房与云 **同城/同区域**，**Gateway 只部署一套在云上** | 自建 GPU §10.5 |
| 20 | Infra Agent **每节点一个**，一律**主动外联**连 Control | 自建 GPU §10.5 |
| 21 | **平台管理员与工作空间成员是两套身份体系**，独立建表 | 本文 §4.5 |
| 22 | 运行时可改的系统设置落 `settings` 表，与 koanf 的启动期配置边界划死 | 本文 §4.5 |
| 23 | 控制平面实际 **55 个端点**（web 21 + admin 34），管理端清单以 §5.5 为准 | 本文 §5.5 |
| 24 | **成本与售价是两本账**，`cost_usd_micro` 拆为三列；金额单位改纳美元 | 计价方案 §2/§3.5 |
| 25 | **价目由 admin 单一写入口录入**，上游同步只产出建议；展示价与计费价同源 | 计价方案 §3.6 |
| 26 | **计费为预付**，余额强一致扣减；企业后付以 `credit_limit` 表达，**首版留字段不产品化** | 计价方案 §6.1 |
| 27 | **自建模型对标云端定固定价**，售价与成本解耦（成本才是摊销值） | 计价方案 §3.6-⑤ |
| 28 | **admin 用独立账号 + 强制 TOTP**，首个 `super_admin` 由 CLI bootstrap | 本文 §4.5 |
| 29 | 缓存命中**跟随上游给折扣**；免费额度**按月重置**；**不做阶梯价**；自建按对标云端 **×0.7** | 计价方案 §11 B/C/D/G |
| 30 | **首版不接支付通道**，充值由 admin 手工入账（走审计、标 destructive） | 计价方案 §11-H |
| 31 | 上游账单**只做差异率监控**（阈值 3%），不逐条对账 | 计价方案 §11-F |
| 32 | 渠道**手工测试不写入健康状态机**，费用记 `upstream_cost` 且不属任何客户 | 统一接口 §5.6 |

**仍未决、但不阻塞的**：上游供应商范围、入口协议范围（建议至少 OpenAI + Anthropic）、是否有数据驻留客户、是否复用 RelayKit、发票与税务（与支付通道一起决策）。
另有三个待给的数字见 [`IMPLEMENTATION-PLAN.md`](./IMPLEMENTATION-PLAN.md) §7。

---

## 1. 最重要的一个架构判断：两个平面必须分开设计

前端的 `api/README.md` 已经写明了这个区分，但它的分量值得在后端再强调一次——这决定了整套架构的形状：

| | 控制平面 | 推理数据平面 |
|---|---|---|
| 路径 | `/api/v1/*` | `/v1/chat/completions` 等 |
| 契约 | 自定义信封 `{data, meta, request_id}` | **OpenAI 兼容，不能包信封** |
| QPS 量级 | 几十 | 几千 |
| 响应形态 | JSON，几 KB | **SSE 流式，持续几十秒** |
| 耗时 | 毫秒级 | 秒级到分钟级 |
| 每请求成本 | ~0 | 真金白银（上游计费） |
| 失败代价 | 刷新重试即可 | 已扣费/已消耗上游配额 |
| 扩容依据 | CPU | **在途连接数** |

两者放在同一个 handler 体系里会出两类问题：控制平面的中间件（比如统一包信封、统一 JSON 序列化）会破坏 OpenAI 兼容性；而数据平面的长连接会把控制平面的连接池吃干净。

**结论**：同一个二进制，两套独立的 router、中间件链与连接池，通过配置决定启用哪个平面。开发期一起跑，规模上来后同一份代码分开部署，不用改代码。

```
                    ┌─────────────────────────────────┐
   web / admin ────▶│  控制平面 :8080  /api/v1/*      │──┐
                    │  信封 · 会话 · 分页 · CRUD       │  │
                    └─────────────────────────────────┘  │
                                                          ├──▶ 存储层
   SDK / Agent  ───▶┌─────────────────────────────────┐  │   (PostgreSQL)
   (OpenAI 兼容)    │  数据平面 :8081  /v1/*          │──┘
                    │  路由 · 转发 · 流式 · 计量       │
                    └──────────────┬──────────────────┘
                                   ▼
                        上游供应商 / 自建 vLLM
```

---

## 2. 技术选型与理由

不罗列技术名词，每一项给出选它的实际原因和放弃的代价。

> 供给侧的适配器抽象（40+ 厂商如何统一）不在本章，见
> [`UNIFIED-PROVIDER-INTERFACE.md`](./UNIFIED-PROVIDER-INTERFACE.md)。

### 2.1 HTTP 框架：Chi

| 候选 | 判断 |
|---|---|
| **Chi** ✅ | 完全兼容 `net/http`，`http.Handler` 直接可用 |
| 标准库 `net/http` | Go 1.22 的 ServeMux 已支持 `GET /models/{provider}/{model}`，但中间件链、分组、参数绑定要自己搭 |
| Gin | 性能好，但自有 `gin.Context`，与标准库生态需要适配 |
| Echo | 居中，无明显优势 |
| Fiber | ❌ **不能选** |

**为什么 Fiber 不能选**：它基于 `fasthttp`，不实现 `net/http` 接口。而数据平面的核心动作是——用 `http.Client` 请求上游、拿到 `resp.Body` 后**边读边往下游 flush**，中间要用 `http.Flusher` / `http.ResponseController`。脱离标准库接口，这条链路上的每一环（SSE 转发、超时控制、连接复用、HTTP/2）都要重写或找替代品。fasthttp 的性能优势在"转发大模型响应"这个场景里根本不是瓶颈——瓶颈在上游。

Chi 的实际收益：路由树支持 URL 参数（契约里 `provider`/`model` 拆成两个路径参数正是为了避开编码斜杠的歧义）、中间件就是 `func(http.Handler) http.Handler`、可以直接复用社区任何 `net/http` 中间件。

### 2.2 数据访问：sqlc + Repository 接口

| 候选 | 优势 | 代价 |
|---|---|---|
| **sqlc** ✅ | 手写 SQL、编译期类型安全、零反射、生成代码可读 | 无法屏蔽方言（本方案只有一个方言，代价消失） |
| GORM | 开发快 | SQL 不可控；分析类查询（时序聚合、窗口函数）会很痛苦；N+1 难察觉 |
| Ent | schema as code、图查询强 | 学习曲线陡；生成代码庞大；复杂聚合仍要落到原生 SQL |
| sqlx | 灵活 | 样板多、无类型安全 |

选 sqlc 的核心理由：这个系统里**最有价值的查询恰恰是最复杂的那些**——按厂商/模型/用户组分组的时序聚合、留存队列、成本归集。ORM 在这些地方帮不上忙，最终还是要写原生 SQL，那不如从一开始就把 SQL 握在手里。

**确定单一 Postgres 后，sqlc 原本最大的代价（双方言各写一套查询，约 20%）消失了**，同时可以放开用 Postgres 独有能力：`jsonb` 操作符、原生数组、`date_trunc`、表分区、`COPY` 批量写、`SKIP LOCKED` 任务队列、`tsvector` 全文搜索。这些在需要兼容 SQLite 时全部要绕开或写两遍。

```
db/queries/       单一目录，不再按方言分叉
db/migrations/    goose，embed 进二进制
```

上层通过 `domain` 包定义的 Repository 接口访问，`store/postgres` 实现该接口。保留接口层不是为了换数据库，而是为了**单元测试能塞内存假实现**，以及让业务代码不直接依赖生成代码。

### 2.3 驱动：pgx/v5

用 `pgx/v5` 原生接口，不套 `database/sql`：pgx 原生支持 Postgres 二进制协议、`COPY`（批量写用量日志）、`LISTEN/NOTIFY`（缓存失效通知）。sqlc 对 pgx 有一等支持。

套 `database/sql` 会丢掉 `COPY` 和二进制协议，而这两项恰好都在热路径上。

### 2.4 Supabase 怎么接：直连 Postgres，不走 PostgREST

Supabase = Postgres + PostgREST + GoTrue + Realtime + Storage。接入方式有两条路：

| 方式 | 判断 |
|---|---|
| **pgx 直连 Postgres** ✅ | Supabase 提供标准连接串，我们的 sqlc 代码原样可用 |
| supabase-go 走 PostgREST | ❌ 多一层 HTTP、查询能力受限、复杂聚合表达不了 |

关键判断：**我们自己就是认证与授权层**，不需要 Supabase 的 RLS（行级安全）。RLS 是为"前端直连数据库"设计的，而我们的前端只跟自己的 Go 服务说话。硬用 RLS 等于把授权逻辑劈成两半，一半在 Go 一半在 SQL 策略里，排查问题时会非常痛苦。

因此 Supabase 在本方案里的定位是**一个托管的 Postgres**，顺带可用它的备份与监控。如果将来要用 Supabase Auth 或 Storage，那是独立的接入点，不影响数据访问层。

连接注意事项：Supabase 默认给的是 **PgBouncer 连接池地址（transaction 模式，6543 端口）**，这个模式下不支持 prepared statement 缓存和会话级特性。用 pgx 时要么关掉 statement cache（`default_query_exec_mode=exec`），要么改用直连端口 5432。这是接 Supabase 最常见的坑，配置里要显式暴露这个开关。

### 2.5 其余选型

| 用途 | 选择 | 理由 |
|---|---|---|
| 迁移 | **goose** | 同时支持两方言；支持 Go 代码迁移（复杂数据迁移用得上）；可 `embed.FS` 打进二进制，私有化部署免带 SQL 文件 |
| 配置 | **koanf** | 无全局状态（viper 的全局单例在测试里很烦）；env + 文件 + flag 分层合并 |
| 日志 | **log/slog** | 标准库，结构化，无第三方依赖。私有化部署少一个依赖是实打实的收益 |
| 追踪指标 | **OpenTelemetry** + Prometheus exporter | admin 的 GPU 监控页已按 Prometheus 指标设计 |
| 校验 | **go-playground/validator** | 契约里有 `minLength`、`format: email`、`const: true` 等约束，需要落地 |
| 任务队列 | **river**（基于 Postgres `SKIP LOCKED`） | 已有 Postgres，不必额外引入 MQ |
| 测试 | 标准库 + testify assert；**全部走 testcontainers 起真实 Postgres**，见 §7 | |

**Redis 是必需组件**（云上部署已确定，此前"不引入中间件"的私有化约束不再适用）。它承担三件多副本下无法用内存解决的事：

| 用途 | 为什么必须是 Redis 而非内存 |
|---|---|
| **配额与余额扣减** | 多副本下内存计数各算各的，必然超额。用 Redis 原子操作（`INCRBY` + Lua 脚本）做单点裁决 |
| **限流** | 同上，滑动窗口计数必须跨副本共享 |
| **会话缓存** | 可用内存 LRU，但 Redis 让副本重启不掉线 |

**但不把 Redis 当真相源**：配额的权威值在 Postgres，Redis 是带 TTL 的加速层与原子裁决点，崩溃后可从 Postgres 重建。这条边界要守住，否则 Redis 一挂就是数据丢失而非降级。

---

## 3. 项目结构

```
backend/
├── cmd/
│   ├── umaas/                  主二进制（可同时或分别启用两个平面）
│   └── umaas-migrate/          迁移工具（也可由主程序 --migrate 触发）
│
├── internal/
│   ├── config/                 分层配置与校验
│   │
│   ├── domain/                 领域模型与 Repository 接口（不含实现）
│   │   ├── model.go            模型、供应商、渠道
│   │   ├── user.go             用户、工作空间、会话、API Key
│   │   ├── usage.go            用量记录、配额、账单
│   │   ├── infra.go            节点、GPU、部署实例（admin）
│   │   └── repository.go       全部 Repository 接口定义
│   │
│   ├── store/                  持久化实现
│   │   ├── postgres/           sqlc 生成 + 接口适配
│   │   └── store.go            连接池、事务封装
│   │
│   ├── httpapi/                控制平面
│   │   ├── router.go
│   │   ├── middleware/         request_id · 会话 · 限流 · 恢复 · CORS
│   │   ├── response/           信封与错误码（契约的 {data,meta,request_id}）
│   │   ├── catalog/            /catalog /models /benchmarks /rankings
│   │   ├── docs/               /docs/*
│   │   ├── auth/               /auth/* /me
│   │   ├── harness/            /harnesses/*
│   │   └── admin/              /admin/*（管理控制台）
│   │
│   ├── gateway/                数据平面
│   │   ├── router.go           OpenAI 兼容路由
│   │   ├── completions.go      chat/completions，含流式
│   │   ├── routing/            渠道选择策略与回退链
│   │   ├── stream/             SSE 解析与转发
│   │   └── meter/              token 计量与计费结算
│   │
│   ├── ir/                     规范表示（协议无关，见统一接口方案 §8）
│   ├── protocol/               入口与上游协议转换 + 质量分级
│   ├── provider/               上游传输层
│   │   ├── profile.go          ProviderProfile + Quirks（约 30 家零代码接入）
│   │   ├── openaicompat/       一个驱动服务全部 OpenAI 兼容厂商
│   │   └── anthropic/ gemini/ bedrock/ vertex/ task/
│   │
│   ├── billing/                配额、余额、账单
│   ├── analytics/              聚合查询（服务 admin 的分析页）
│   ├── infra/                  私有化部署编排（admin）
│   │   ├── gpu/                DCGM 指标抓取
│   │   ├── registry/           模型权重拉取
│   │   ├── deploy/             实例生命周期
│   │   └── bench/              压测执行
│   └── platform/               跨领域基础设施（ID 生成、时钟、加密、SSE 写入器）
│
├── db/
│   ├── migrations/             goose 迁移，embed
│   └── queries/                sqlc 查询（单一 Postgres 方言）
│
├── api/
│   └── openapi.admin.yaml      管理端契约（按 §5.5 的清单补齐，与前端 contracts 对齐）
│
├── sqlc.yaml
├── go.mod
└── Makefile
```

**关于 `internal/`**：全部业务代码放在 `internal/` 下，Go 的可见性规则会阻止外部模块导入。这不是洁癖——它保证将来把某个包挪走时不会有人已经依赖了它。只有真正要给外部用的（比如 SDK）才放 `pkg/`，目前一个都没有。

**领域接口与实现分离的实际收益**：`domain/repository.go` 定义接口，`store/postgres` 实现。业务代码只依赖接口，于是单元测试可以直接塞内存假实现，而不必为每个测试起数据库。这一层不是为了"将来换数据库"——那是个很少兑现的承诺——而是为了测试可控与依赖方向清晰。

---

## 4. 数据模型

按领域分组，只列关键表与设计要点。

### 4.1 身份与租户

```
users            id, email, name, password_hash, avatar_url, status, created_at
workspaces       id, name, slug, plan, seats_limit, monthly_budget_usd, balance_usd
memberships      user_id, workspace_id, role(owner|admin|developer|viewer)
sessions         id, user_id, expires_at, ip, user_agent, revoked_at
api_keys         id, workspace_id, name, key_hash, key_prefix, scope, last_used_at
oauth_identities user_id, provider, provider_uid, UNIQUE(provider, provider_uid)
```

- **`api_keys` 只存哈希与前缀**。契约里写明"密钥仅展示一次"，那服务端就不该有能力再展示第二次。前缀（`um_live_8f2a`）用于列表展示与日志定位，哈希用于校验。
- 会话表带 `revoked_at` 而非直接删除：`/auth/session` 要能区分"没登录"和"会话已被踢下线"。
- `balance_usd` 用**整数**存储，不用浮点。契约说"Money values are numeric USD amounts"，但那是 JSON 表示；内部存储必须避开二进制浮点的累积误差。
  **单位已由计价方案修正为纳美元（`balance_nano`）**——分和微美元在单 token 粒度上都不够，理由见 [计价与计费方案 §3.5](./BILLING-AND-PRICING.md)。余额的真相在 `credits` 流水表，此列是物化汇总（该文 §6.2）。

### 4.2 模型目录与供给

> ⚠️ **本节的表结构以 [统一接口方案 §5.2](./UNIFIED-PROVIDER-INTERFACE.md) 为准。**
> 那里基于对 new-api `Ability` 表与 freellmapi `media_models` 的分析做了两处修正：
> 模型按模态拆表、路由索引改用正规化关联表。此处只列摘要，改动请改那一处。

```
providers          id, slug, name, kind(cloud|self_hosted)
provider_profiles  协议族 + base_url + 鉴权方式 + quirks（约 30 家零代码接入的载体）
models             文本/对话模型：context_length, capabilities, 定价, status…
media_models       图像/音频/视频模型：modality, spec(jsonb), status…
channels           id, provider_profile_id, credentials_enc, region,
                   priority, weight, status, health_state
channel_models     channel_id, model_id, upstream_model_name, enabled, 权重覆盖
group_models       分组可见性，与路由解耦
model_stats        model_id, day, tokens, requests, p95_latency_ms（物化聚合）
upstream_diffs     id, source, fetched_at, payload, applied_at, applied_by
```

- **模型 ID 是 `provider/model`**，但表里拆成 `provider_id` + `slug`。契约特意把 HTTP 路径拆成两个参数就是为了避开编码斜杠，数据层保持一致。
- `credentials_enc`：上游密钥**加密存储**（AES-GCM，密钥来自环境变量或 KMS），不明文落库。
- `upstream_diffs` 表持久化差异审阅记录——admin 的差异审阅是"先审后用"，那么"抓取了什么/审了什么/谁应用的"必须留痕，否则改价这类操作出问题时无从追溯。

### 4.3 用量与计费

> ⚠️ **本节的计费相关字段以 [计价与计费方案 §9](./BILLING-AND-PRICING.md) 为准。**
> 那里做了三处修正：`cost_usd_micro` 拆为 `upstream_cost_nano` / `charged_amount_nano`
> （成本与售价是两本账，前者可回填、后者不可变）、金额单位由微美元改为纳美元
> （micro 在单 token 粒度上已经截断）、`request_logs` 改为 attempt 级记录以容纳重试成本。
> 此处只列摘要，改动请改那一处。

```
request_logs     id, workspace_id, api_key_id, model_id, channel_id,
                 prompt_tokens, completion_tokens, cache_read_tokens,
                 cache_write_tokens, tool_call_count, cost_usd_micro,
                 ttft_ms, latency_ms, status_code, error_code,
                 agent_framework, created_at
usage_rollups    workspace_id, model_id, day, tokens_*, requests, cost_usd_micro
quotas           workspace_id, period, tokens_limit, requests_limit, budget_usd
invoices/topups  ...
```

- **`cost_usd_micro` 用微美元（1e-6）整数**。单次请求成本常在 $0.0001 量级，浮点累加百万次后误差会进入账单。
- `request_logs` 是全系统写入压力最大的表，且**只追加不更新**。用 Postgres 原生分区 `PARTITION BY RANGE (created_at)` 按日或按周切分，配合保留策略（契约写明元数据保留 30 天）自动 `DETACH` 旧分区。分区让删除历史数据从一次全表 `DELETE` 变成一次 `DROP TABLE`。
- `usage_rollups` 是预聚合表。分析页要查"近 30 天按厂商分组的 tokens"，直接扫 `request_logs` 在千万行级别会退化。由后台任务按 UTC 日聚合——契约里明确"Analytics are aggregated by complete UTC periods"，正好对上。
- `agent_framework` 字段现在会是空的。admin 的工具调用页把这个维度做成了空状态，因为**网关还没有这个埋点**。字段先留好，埋点上线后自然填充（见 §6.4）。

### 4.4 私有化部署（admin）

```
nodes            id, hostname, gpu_model, gpu_count, interconnect, labels
gpus             node_id, index, uuid, memory_total_mb, status, allocated_to
local_models     id, repo, revision, params_b, precision, size_bytes,
                 sha256_verified, storage_path, source
download_tasks   id, repo, total_bytes, downloaded_bytes, shards, status
deployments      id, name, model_id, engine, node_id, gpu_indices,
                 tensor_parallel, precision, max_model_len, replicas, status
benchmark_runs   id, deployment_id, slo_ttft_ms, slo_tpot_ms, points(json), knee_concurrency
audit_logs       id, actor, action, target, destructive, detail, ip, created_at
```

- GPU 的实时指标（利用率、温度、功耗）**不入库**，从 Prometheus 查。入库的只有拓扑与分配关系这类慢变数据。把秒级指标写进业务库是常见错误——那是时序库的活。
- `audit_logs.destructive` 是布尔标记。admin 的审计页支持"仅看破坏性操作"一键筛选，这个筛选要走索引，所以做成独立字段而不是从 action 字符串推断。

### 4.5 平台管理身份与系统设置

这一节补的是 §5.5 清单里 `/admin/system/*` 那一组的数据模型——此前全系统没有它们，而它们不是 CRUD 的补充，是**两个独立的身份/配置体系**。

**平台管理员不是"某个工作空间的 admin"。**

`memberships.role(owner|admin|developer|viewer)`（§4.1）是**工作空间内**的角色：管自己的 API Key、看自己的账单。而 admin 的 `AdminRole(super-admin|operator|viewer)` 是**平台级**的：看所有客户的账单、改模型价格、删部署实例、停用用户。

把两者合并很有诱惑力（"给某个内部工作空间的 owner 加个 is_staff 标记"），但它错在授权模型：平台权限的作用域是**全局**，而 `memberships` 的每一条都锚定一个 `workspace_id`。合并后每次鉴权都要问"这个 admin 角色是对哪个工作空间说的"，而正确答案是"跟工作空间无关"。

```
admin_accounts   id, email, name, role(super_admin|operator|viewer),
                 password_hash, totp_secret_enc, totp_enabled,
                 status, last_active_at, created_by, created_at, disabled_at
admin_sessions   id, admin_id, expires_at, ip, user_agent, revoked_at
```

- **与 `users` / `sessions` 是独立的表**，不共用。作用域不同；且 admin 会话的策略应当更严（更短 TTL、强制 2FA、可选 IP 白名单），共表会逼着两套策略在同一份代码里分支。
- `AdminAccount.twoFactor` 在前端已是一等字段，因此 `totp_secret_enc` 必须加密存储（与 `channels.credentials_enc` 同一套 AES-GCM）。**已定（2026-09-08）：全员强制 2FA**，不提供关闭入口——该字段因此恒为 true，保留它只为界面显示绑定状态。
- `created_by` 不可为空：管理员账号只能由管理员创建，没有自助注册入口。**首个 `super_admin` 由 CLI 创建**（`umaas admin create --super`），这是唯一的例外，也是首次部署的必经步骤——**不写进部署文档，第一次上线会卡在没人能登录**。
- 需要一组 `/admin/auth/*` 端点（登录、登出、会话查询、TOTP 绑定与校验），前端 `services.ts` 里目前一个都没有，写 `openapi.admin.yaml` 时一并补齐。
- 停用走 `disabled_at` 而非删除行——审计日志里的 `actor` 要能一直解析出人名。因此 `DELETE /admin/system/admins/{id}` 的语义是**停用**而非物理删除，**这一条必须写进契约描述**，否则实现者会照字面做成硬删，然后历史审计记录里出现一批查不到人的 actor。

**系统设置需要一张表，koanf 管不了它。**

§2.5 选的 koanf 是**启动期**配置（env + 文件 + flag），而 admin 的 `SettingGroup` 是**运行时可改**的配置项（前端已定义 `switch | number | select | text` 四种控件及 `options` / `unit`）。两者不是一回事，不能指望一个覆盖另一个。

```
settings         key, value jsonb, kind, requires_restart, updated_by, updated_at
```

三条约束：

- **每一项必须声明是否需要重启才生效**，并在界面上标出来。一个改了没反应的开关比没有这个开关更糟——运维会以为它生效了。
- **写入全部走审计**，其中"影响计费/路由/配额"的项标 `destructive = true`。
- **启动期配置与运行时配置的边界要划死**：数据库连接串、监听端口、加密密钥只能来自 koanf，永远不进 `settings` 表；限流阈值、默认路由策略、告警开关才放表里。**边界不划死，迟早有人把数据库密码做成一个能在网页上编辑的输入框。**

---

## 5. 控制平面实现要点

### 5.1 响应信封

契约规定了统一信封，且**成功与失败都要带 `request_id`**：

```go
type Envelope[T any] struct {
    Data      T      `json:"data"`
    Meta      *Meta  `json:"meta,omitempty"`
    RequestID string `json:"request_id"`
}
```

错误码语义已在契约里定死（400/401/403/404/409/422/429/5xx），实现时用一个集中的错误映射，而不是在每个 handler 里手写状态码——否则同一类错误在不同端点返回不同状态码是迟早的事。

领域层返回带语义的错误（`domain.ErrNotFound`、`domain.ErrConflict`、`domain.ErrValidation`），HTTP 层统一翻译。领域层不认识 HTTP 状态码。

### 5.2 游标分页

契约用 `cursor` + `limit` + `meta.next_cursor`，不是 offset。这是对的——offset 分页在深翻页时会全表扫描，而且数据插入会导致跨页重复/遗漏。

游标编码为不透明字符串（base64 的 `{sort_key, id}`），**必须包含 id 作为决胜字段**，否则排序键相同的行会在翻页时错乱。

### 5.3 会话认证

契约要求 `HttpOnly` + `Secure` + `SameSite=Lax` cookie，并明确写了"Access tokens are not stored in localStorage"。

- 会话 ID 用密码学随机数（32 字节），存储的是**哈希后**的值——数据库泄露不等于会话被劫持。
- 会话查询走内存 LRU 缓存（TTL 短），命中率会很高，避免每个请求打一次库。
- `SameSite=Lax` 意味着跨站 POST 不带 cookie，OAuth 回调要用 GET 重定向（契约里的 `/auth/oauth/{provider}` 正是 302 重定向）。

### 5.4 缓存策略

契约的 README 已经给了每类资源的缓存策略（catalog `max-age=60`、benchmarks `max-age=300`、session `no-store`）。这些直接落到响应头，并配 `ETag`——docs 类资源尤其值得，前端会频繁请求且内容很少变。

### 5.5 管理端端点清单与数据模型归属

此前 `/admin/*` 在本文里只有一个占位（§3 目录树里的一行），而前端 `admin/src/api/services.ts` 已经定义了**全部 34 个端点**。这一节把它们逐条列出并标注归属，**它是 `api/openapi.admin.yaml` 的编写依据**。

> **两份清单已经漂移。** 前端方案 `ADMIN-PLATFORM-UI-DESIGN.md` §9.3 列了 13 条（只覆盖私有化部署），
> 但 `services.ts` 的实际调用与它并不一致：`services.ts` 多了 `deployments/preflight`、
> `benchmark/runs/latest`，少了 `deployments/{id}/logs`、`{id}/scale`、`capacity/estimate`
> （容量评估已确认为纯前端计算，见 `SELF-HOSTED-GPU-OPERATIONS.md` §11.6）。
> 两份都不是权威——**以本节为准**，实现时以 `services.ts` 的实际调用为交叉验证。

| 端点 | 方法 | 数据模型归属 | 状态 |
|---|---|---|---|
| `/admin/overview` | GET | `usage_rollups` + `request_logs` 聚合 | ✅ 有支撑 |
| `/admin/analytics/usage` | GET | 同上，按 `dimension`/`measure`/`days` 切 | ✅ |
| `/admin/analytics/users` | GET | `users` / `workspaces` + 留存队列 | ✅ |
| `/admin/analytics/models` | GET | `model_stats` + 成本口径见计价方案附 1.5 | ⚠️ 成本需带 `cost_kind` |
| `/admin/catalog/models` | GET | `models` / `media_models`（统一接口 §5.2） | ✅ |
| `/admin/catalog/models/{id}/delist` | POST | `models.status` 状态机（计价方案 §3.6-③） | ⚠️ 状态机此前未定义 |
| `/admin/catalog/upstream-diff` | GET | `upstream_diffs`（§4.2） | ✅ |
| `/admin/catalog/upstream-diff/apply` | POST | 写 `model_prices` 新版本（计价方案 §3.6-①） | ✅ 已补 |
| `/admin/catalog/channels` | GET | `channels` / `channel_models`（统一接口 §5.2） | ✅ |
| `/admin/catalog/channels/{id}/test` | POST | **无设计**：连通性探测的口径、超时、是否计费 | ❌ 缺口一 |
| `/admin/catalog/routing` | GET | **无表**：策略是可编辑对象（统一接口 §5.5） | ❌ 缺口二 |
| `/admin/infra/nodes` | GET | `nodes` / `gpus`（§4.4） | ✅ |
| `/admin/infra/gpus/metrics` | GET | 转发 Prometheus，不入库（§4.4） | ✅ |
| `/admin/infra/registry/models` | GET | `local_models` | ✅ |
| `/admin/infra/registry/resolve` | POST | 无状态，解析社区仓库元数据 | ✅ |
| `/admin/infra/registry/pull` | POST | `download_tasks` | ✅ |
| `/admin/infra/registry/pull/{id}` | DELETE | 同上 | ✅ |
| `/admin/infra/deployments` | GET / POST | `deployments`（自建 GPU §11.4） | ✅ |
| `/admin/infra/deployments/preflight` | GET | 部署预检（自建 GPU §7、§11.4） | ✅ |
| `/admin/infra/deployments/{id}` | DELETE | drain 后回收（自建 GPU §11.4） | ✅ |
| `/admin/infra/benchmark/runs` | GET | `benchmark_runs` | ✅ |
| `/admin/infra/benchmark/runs/latest` | GET | 同上 | ✅ |
| `/admin/customers/users` | GET | `users` + 汇总 | ✅ |
| `/admin/customers/users/{id}/suspend` | POST | `users.status` | ✅ |
| `/admin/customers/workspaces` | GET | `workspaces` + 席位/预算 | ✅ |
| `/admin/customers/billing` | GET | `invoices` / `topups`（计价方案 §7） | ✅ 已补 |
| `/admin/customers/topups/{id}/refund` | POST | `topups.status='refunded'` + `credits` 负向调整（计价方案 §6.2） | ✅ 已补 |
| `/admin/system/settings` | GET / PATCH | `settings`（§4.5） | ✅ 已补 |
| `/admin/system/admins` | GET | `admin_accounts`（§4.5） | ✅ 已补 |
| `/admin/system/admins/{id}` | DELETE | **语义是停用**，写 `disabled_at`（§4.5） | ✅ 已补 |
| `/admin/system/audit` | GET | `audit_logs`（§4.4） | ✅ |

**两个缺口均已补齐**（2026-09-08），补在各自的真相源里：

- **渠道连通性测试** → `UNIFIED-PROVIDER-INTERFACE.md` **§5.6**。要点：探测模型的选取优先级、请求形态（`max_tokens=1`、非流式、不重试）、费用记 `upstream_cost` 且 `workspace_id=NULL`、**手工测试的结果不写入健康状态机**（样本有偏且可被无意操纵）、限频、返回分项结果而非一个 bool。
- **路由策略无处存储** → `UNIFIED-PROVIDER-INTERFACE.md` **§5.5**，`routing_policies` 表已补。

**关于 B6 的工期**：管理端 34 个端点比面向用户的 21 个还多，且其中 11 个依赖 B7 的私有化部署模块。B6 那一行不是"补一批 CRUD"，实际是本项目最大的单个阶段。

---

## 6. 数据平面实现要点

这是全系统技术难度最高的部分。

> **统一服务接口是本章的核心难题，已单独展开为 [`UNIFIED-PROVIDER-INTERFACE.md`](./UNIFIED-PROVIDER-INTERFACE.md)。**
>
> 该文档基于对 `opensource/new-api`（41 个厂商适配器 + RelayKit 协议转换模块）与
> `opensource/sub2api`（渠道健康监控）的源码分析，回答 40+ 厂商、1000+ 模型、
> 多模态如何收敛为一套接口。核心结论：把「入口协议 × 能力类型 × 上游协议 ×
> 传输形态 × 模型」的笛卡尔积拆成四层加法，其中约七成厂商是 OpenAI 兼容的，
> 应当**零代码接入**（配置即可）。
>
> 本章以下内容是该方案在数据平面的落地约束，两者需对照阅读。

### 6.1 流式转发

```
客户端 ──POST /v1/chat/completions {stream:true}──▶ 网关
                                                    │ 选渠道
                                                    ▼
                                              上游 (SSE)
                                                    │
        ◀── 边收边转发，每个 chunk 立即 flush ──────┘
```

关键实现约束：

- 用 `http.ResponseController.Flush()`（Go 1.20+），比类型断言 `http.Flusher` 更稳妥。
- **禁止在中间做缓冲**。任何一层缓冲都会把"流式"变成"慢的非流式"，用户会看到长时间空白后一次性吐出全部内容。这也是不能选 Fiber 的原因之一。
- 反向代理层（nginx/Traefik）必须关掉响应缓冲（`proxy_buffering off`），否则应用层做对了也白搭。这一条要写进部署文档。
- 客户端断开时要**主动取消上游请求**（`ctx` 传播），否则上游还在计费而没人接收。这是真金白银的浪费。

### 6.2 一个无法回避的难点：流开始后不能重试

回退链在非流式请求里很好做——失败了换个渠道重来。但流式请求一旦发出第一个 chunk，客户端已经收到部分内容，此时上游断了**没法重试**：重试会导致内容重复或不连贯。

应对：

1. **把失败尽量前移**。发往上游后、第一个 chunk 到达前的这个窗口内，失败仍可安全重试。这需要**两个独立的流式超时**——只设首字节超时不够，流已开始后上游卡住不再吐字节时它早已失效：

   | 超时 | 覆盖阶段 | 超时后 |
   |---|---|---|
   | `firstByteTimeout`（如 10s） | 发出请求 → 首个 chunk | 仍在安全窗口，**换渠道重试** |
   | `stallTimeout`（如 30s） | 相邻两个 chunk 之间 | 已过安全窗口，只能中止并如实上报 |

   参数细节与 HTTP 客户端的超时边界设置见 **统一接口方案 §7**（真相源）。
2. **首 chunk 之后的失败据实上报**。以 SSE error 事件告知客户端，并在 `request_logs` 里标记 `partial_failure`，计费按实际产出的 token 计算。
3. **不要假装成功**。有的网关会在中断时静默补一个 `finish_reason: stop`，这会让客户端以为拿到了完整回答——比明确报错有害得多。

### 6.3 计量与计费：不能每请求同步写库

> **计费的完整设计已单独展开为 [`BILLING-AND-PRICING.md`](./BILLING-AND-PRICING.md)。**
> 本节只定"写入路径按一致性分流"这一条策略；价目模型、预扣估算公式、reservation 的
> 悬挂回收、对账的三个参数（周期 / 等式 / 告警阈值）全部以该文为准。
> 尤其注意：本节末尾"不会因此产生坏账"的论证**只覆盖日志批量写的丢失窗口**，
> 预扣的悬挂窗口是另一个坑，见该文 §5.3。

每个推理请求都要记一条日志、扣一次配额。同步写库会让数据库成为吞吐瓶颈。

分成两条路径，按一致性要求区别对待：

| 数据 | 一致性要求 | 方案 |
|---|---|---|
| **配额/余额扣减** | 强 | **Redis 原子操作裁决**（Lua 脚本保证检查与扣减原子），Postgres 为真相源并周期性对账。**宁可慢一点也不能超额** |
| **请求日志** | 弱 | 异步 channel → `COPY` 批量写入。flush 间隔 1–5s |
| **用量聚合** | 弱 | 后台任务按 UTC 日滚动，写 `usage_rollups` |

批量写的丢失窗口等于 flush 间隔——进程被 kill 时最多丢几秒日志。这是**刻意接受的权衡**：日志用于分析和对账，秒级窗口的丢失可容忍；而余额扣减不走这条路，所以不会因此产生坏账。

关于任务队列：用 **river**（基于 Postgres `SKIP LOCKED`）。已经有 Postgres 了，任务队列这个量级不值得再引入一套 MQ 的运维成本；真到了 MQ 才能解决的规模再换不迟。

### 6.4 Agent 框架埋点（当前缺口）

admin 的工具调用分析页有一个维度是空的：按 Agent 框架（Claude Code / uCodex / OpenCode / Pi）分布。原因是网关**没有记录调用方框架**。

补法：数据平面从请求里提取标识并写入 `request_logs.agent_framework`：

1. 优先读自定义头 `X-UMaaS-Client`（需要 SDK/客户端配合）
2. 回退到 `User-Agent` 的模式匹配（各 Agent 框架的 UA 有特征）
3. 都识别不出记为 `unknown`，**不猜**

第 3 条很重要：宁可有一大块 `unknown`，也不要用启发式硬凑出一个看起来合理的分布。admin 那个页面现在保持空状态就是这个道理——假数据会被拿去做决策。

---

## 7. 为什么只用 Postgres，连测试也不用 SQLite

云上部署已确定，SQLite 退回"临时测试方案"的定位。**本方案的建议是再进一步：彻底不用它。**

保留 SQLite 只用于测试，看起来省事，实际要付两笔账：

**第一笔是显性的**——sqlc 需要为两个方言各写一套查询，约 20% 的查询会分叉（JSON 读取、时间截断、数组、全文搜索、UPSERT 细节）。这部分工作量在只跑 Postgres 时完全不存在。

**第二笔是隐性的，而且危险得多**：

> **用 SQLite 跑测试、Postgres 跑生产，等于把方言相关的 bug 全部排除在测试覆盖之外。**

具体会漏掉什么：

| 差异 | 测试（SQLite）表现 | 生产（Postgres）表现 |
|---|---|---|
| 并发写 | 单写者串行，天然无竞争 | MVCC，**真实的死锁与序列化失败** |
| 事务隔离 | 实质接近串行化 | 默认 Read Committed，幻读可发生 |
| 类型严格性 | 动态类型，字符串存进整数列也接受 | 直接报错 |
| 时间语义 | 无原生 timestamp，存文本 | `timestamptz` 带时区换算 |
| 表分区 | 不支持，测不到 | `request_logs` 的核心机制 |
| `COPY` 批量写 | 不支持，测不到 | 用量日志的热路径 |

**测试全绿而生产炸掉**，正是这种配置的典型产出。而 `request_logs` 的分区和 `COPY` 恰恰是我们写入压力最大的路径——最需要测试的地方反而测不到。

**替代方案**：`testcontainers-go` 起真实 Postgres。现在的实际体验是——容器复用（`Reuse: true`）下首次几秒、后续毫秒级；CI 上 GitHub Actions 有原生 service container 支持。为了省这几秒而换来一个测不准的测试套件，不划算。

```go
// 单元测试：塞内存假实现，不碰数据库，毫秒级
// 集成测试：testcontainers 起真实 PG，与生产同构
// CI：service container，全量跑
```

**唯一值得保留 SQLite 的场景**是"开发者本地零依赖启动"。但即便这个诉求，用 `docker compose up -d postgres` 一行也能满足，且开发环境与生产同构本身就是收益。**建议不保留。若坚持保留，必须限定它只在本地开发可用，CI 与所有测试一律走 Postgres**——否则第二笔账照样要付。

### 规模上限与演进

单实例 Postgres 的瓶颈会最先出现在 `request_logs` 的写入上。演进路径已经清楚，不必现在做：

1. 分区 + 保留策略（首版就做）
2. 只读副本承接分析类查询（读写分离）
3. `request_logs` 迁到列存（ClickHouse），主库只留元数据与聚合结果

前两步是配置层面的事，第三步才涉及架构改动。触发条件是分析查询开始影响在线写入。

## 8. 部署形态

> **是否采用微服务架构，已单独评估为 [`SERVICE-DECOMPOSITION.md`](./SERVICE-DECOMPOSITION.md)。**
>
> 结论摘要：**只有推理数据平面（Gateway）是"不拆是错的"**，它三条判据全中（性能特征、
> 故障域、变化率）。Worker 与 Infra Agent 按触发条件拆。**Control API 保持单体**——
> catalog/auth/billing 之间有事务需求，拆开就要分布式事务，而计费不能接受最终一致。
>
> 关键做法是**同一份代码支持单进程与多进程两种装配**（跨服务调用走接口，本地/gRPC 双实现），
> 让本节的"私有化单机"与云上微服务同时成立。本节以下三种形态是该方案的 `monolith`
> 与 `split` 两种拓扑的具体化。

两种，同一份代码：

| 形态 | 配置 | 组件 |
|---|---|---|
| **本地开发** | 单进程，两平面同开 | 二进制 + `docker compose` 起 Postgres/Redis |
| **云上生产** | 数据平面与控制平面分开部署，各自多副本 | 二进制 ×N + Supabase/RDS + Redis |

早期两者可以是同一个进程（`monolith` 拓扑），规模上来后按 [`SERVICE-DECOMPOSITION.md`](./SERVICE-DECOMPOSITION.md) 的路径拆分，业务代码不变。

迁移文件用 `embed.FS` 打进二进制，但**生产环境不在服务启动时自动执行**——多副本同时启动会竞争迁移锁，且一次失败的迁移会让整个服务起不来。正确做法是把迁移作为部署流水线的独立步骤（`umaas-migrate up`），迁移成功后再滚动更新服务。开发环境可以自动执行以图方便。

前端产物由 nginx/CDN 托管，`/api/v1/*` 与 `/v1/*` 反代到后端。**admin 建议单独域名或 IP 白名单**，不与公网门户同域暴露——这一点在前端方案 §10.1 里已经定了。

---

## 9. 分期实施

> **任务级的分解、依赖顺序与迭代边界已单独展开为 [`IMPLEMENTATION-PLAN.md`](./IMPLEMENTATION-PLAN.md)。**
>
> 本节的 B0–B7 是**阶段**，那份文档把它与其余四条子序列（统一接口 P1–P7、计价 M1–M7、
> 自建 GPU G1–G7、服务拆分 S0–S4）合并成 **I0–I9 十个迭代**，并裁决了三处排期冲突——
> 其中最要紧的是：**M1/M2 必须紧跟 B3，不能等到 B5**（`request_logs` 与价目表一旦有生产数据，
> 改动就从改代码变成数据迁移 + 历史账单重算）。另有八项跨领域任务此前不属于任何序列，
> 在那里记为 X 系列。

| 阶段 | 内容 | 交付判据 |
|---|---|---|
| **B0** | 骨架：config、store 抽象、迁移、信封与错误映射、日志与 request_id、健康检查 | 两种驱动都能启动并通过迁移；`/healthz` 可用 |
| **B1** | 身份：注册/登录/会话/登出/OAuth/`/me` | web 的登录流程能真跑通，不再用 mock |
| **B2** | 目录只读：`/catalog/summary`、`/models`、`/models/{p}/{m}`、`/benchmarks`、`/rankings`、`/docs/*` | web 首页与模型页脱离 `data.ts` |
| **B3** | 数据平面 MVP：`/v1/chat/completions`（含流式）、单渠道直连、请求日志 | 能用 OpenAI SDK 调通，SSE 正常 |
| **B4** | 路由与回退：多渠道、权重、健康检查、自动降权 | 主渠道故障时自动切换 |
| **B5** | 计量计费：配额、余额、用量聚合、API Key（细分为 M1–M7，见[计价方案 §10](./BILLING-AND-PRICING.md)） | 用量数字与实际调用对得上；并发压测不超额 |
| **B6** | 管理端：`/admin/*` **34 个端点**（清单见 §5.5）+ 平台管理身份与系统设置（§4.5） | admin 关掉 `VITE_USE_MOCK` 后可用；管理员可用独立账号 + 2FA 登录 |
| **B7** | 私有化部署模块：GPU 指标、模型拉取、部署编排、压测 | admin 的 infra 五页接真实数据 |

**B3 建议早做**。它是整个产品的价值核心，也是技术风险最高的部分（流式、超时、取消、计费）。把风险前置，比先做完一堆 CRUD 再发现流式转发有坑要好。

---

## 10. 需要确认的问题

以下几点会实质影响实现，我按当前理解做了默认假设：

> ~~**A. 首版数据库到底以哪个为主？**~~ **已定（2026-09-06）**：云服务部署，PostgreSQL/Supabase 为唯一生产库。
> 本方案据此进一步建议**连测试也不用 SQLite**，理由见 §7。已连带解除两条原有约束：
> Redis 改为必需组件、不再要求单二进制零中间件交付。

**B. 上游供应商范围。** 首版接哪几家？OpenAI / Anthropic / Google 三家的 API 差异不小（尤其流式事件格式与工具调用协议），每家适配器都是实打实的工作量。自建 vLLM/SGLang 因为是 OpenAI 兼容，可复用 openai 适配器。

> ~~**C. 计费模型。**~~ **已定（2026-09-08）**：**预付**。余额按强一致扣减（本文 §6.3 的 Redis 原子裁决 +
> Postgres 对账），企业月结用 `workspaces.credit_limit_nano` 表达，**首版建列但保持 0、不做产品化**——
> 该列无论如何都必须存在，[计价方案 §5.3](./BILLING-AND-PRICING.md) 的补记路径要求余额可短暂为负。
> B5 就此解除阻塞，实施细分见该文 §10 的 M1–M7。
>
> **附带未决**：前端 console 已画"自动充值"，它需要支付通道。**首版若不接支付，充值只能由 admin 手工入账**，
> 前端要相应处理该入口的状态。这不阻塞 B5 的计量部分。

**D. 是否需要多租户隔离到数据库级别？** 当前设计是共享库 + `workspace_id` 过滤。如果有客户要求数据物理隔离，架构要改（schema per tenant 或 db per tenant）。

**E. admin 契约还没写进 openapi。** 前端 `admin/src/api/services.ts` 里已定义了全部 `/admin/*` 端点路径，但仓库级 openapi 里还没有。建议在 B0 阶段补齐 `api/openapi.admin.yaml`，让两端契约都有唯一真相源。

> **已补（2026-09-08）**：端点清单与数据模型归属见 §5.5，缺失的数据模型见 §4.5。
> 写 `openapi.admin.yaml` 时以 §5.5 为依据。
>
> **管理端认证已定（2026-09-08）**：**独立账号 + 强制 TOTP**，即 §4.5 的 `admin_accounts` /
> `admin_sessions` 两表，**全员强制 2FA**（不只 `super_admin`），首个 `super_admin` 由
> `umaas admin create --super` 从 CLI 创建，无网页注册入口。B6 就此解除阻塞。
> **但契约缺口仍在**：`services.ts` 里没有任何 login/session 调用，`/admin/auth/*` 这组端点
> 前后端都还没定义——写 `openapi.admin.yaml` 时要一并补上（登录、登出、会话查询、TOTP 绑定与校验）。
>
> 仍未闭合的一处是**渠道连通性测试的口径**（§5.5 缺口一）。

> **已定（2026-09-06）**：运营自有 GPU 集群。自建 vLLM 成为一等供给来源，
> 由此产生的容量、成本与混合路由问题见 [`SELF-HOSTED-GPU-OPERATIONS.md`](./SELF-HOSTED-GPU-OPERATIONS.md)。
> 编排底座已定：**裸机 + Docker，首版只上 vLLM**。实现方案见该文 §11——
> 其中 docker-compose 仅用于平台服务（固定拓扑），推理实例由 Agent 经 Docker API 动态创建。

> ~~**F. 前端方案 §11 的 B/C/D**~~ **已定**：推理引擎 vLLM（单选）、编排底座裸机 + Docker、
> 监控 DCGM + vLLM `/metrics` 双层。§4.4 的表结构与 `internal/infra/` 据此实现，
> 细节见 [`SELF-HOSTED-GPU-OPERATIONS.md`](./SELF-HOSTED-GPU-OPERATIONS.md) §11。

**F. 自建 GPU 是自有机房还是云上实例？** 这是当前唯一还影响架构的未决项——它决定预热池的经济性算法（自建 GPU 运营方案 §3）。其余待确认项均为产品/范围决策，不阻塞 B0–B3。

---

## 附：与前端的对接检查表

给实现者的对照清单，每个端点交付前核对：

- [ ] 响应包信封 `{data, meta?, request_id}`，成功与错误都有 `request_id`
- [ ] 错误码符合契约的语义表（409 = 资源冲突，422 = 字段校验失败）
- [ ] 列表端点用游标分页，返回 `meta.next_cursor`
- [ ] 数组参数用重复 query key（`?modality=text&modality=image`）
- [ ] 模型 ID 在路径上拆为 `{provider}/{model}` 两段
- [ ] 时间是 ISO 8601 UTC；金额是数值不是展示字符串
- [ ] 分析类响应带 `meta.updated_at` 供前端标注数据新鲜度
- [ ] 缓存头符合 README 的策略表；`ETag` 用于 docs
- [ ] 会话 cookie 是 `HttpOnly; Secure; SameSite=Lax`
- [ ] 数据平面响应**不包信封**，保持 OpenAI 原样兼容
