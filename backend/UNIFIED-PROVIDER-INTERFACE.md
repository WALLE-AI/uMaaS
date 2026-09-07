# 统一服务接口技术方案

> 状态：待评审 · 2026-09-06
> 问题：40+ 模型厂商、1000+ 模型、文本/语音/图像/视频/嵌入/重排多种模态，如何收敛为一套接口
> 参考实现：`opensource/new-api`（含 RelayKit 子模块）、`opensource/sub2api`、`opensource/freellmapi`
> 归属：`backend/ARCHITECTURE.md` §6 数据平面的展开（文档边界见该文「文档关系」一节）
>
> **本文是以下内容的真相源**：协议转换与质量分级、适配器抽象、模型注册表与路由索引、
> 渠道评分与回退、流式超时参数。这些在 `ARCHITECTURE.md` 中只有摘要与指向。
> **本文受以下约束**：`ARCHITECTURE.md` 已定的技术栈（Go / Chi / sqlc / pgx）、
> **单一 PostgreSQL 生产库**、Redis 为必需组件、计费不跨服务边界（见 `SERVICE-DECOMPOSITION.md` §1）。

---

## 1. 问题的真正形状

"统一 40 个厂商"听起来是写 40 个适配器的体力活，但真正的成本不在这里。把问题拆开看，实际有五个彼此正交的维度：

| 维度 | 取值 | 数量级 |
|---|---|---|
| **入口协议** | 客户端用什么格式说话：OpenAI Chat / OpenAI Responses / Anthropic Messages / Gemini | 4+ |
| **能力类型** | 要做什么：chat / embedding / rerank / tts / stt / image / video / realtime | 8+ |
| **上游协议** | 厂商用什么格式：OpenAI 兼容 / Anthropic / Gemini / Bedrock / 自有 | 5+ |
| **传输形态** | 同步 / SSE 流式 / WebSocket / 异步任务（提交+轮询） | 4 |
| **模型** | 具体型号及其能力矩阵 | 1000+ |

朴素做法是为每个组合写代码，那是 4×8×5×4 的笛卡尔积。**方案的全部意义就是把这个乘法拆成加法。**

一个具体的成本标尺，用来检验设计是否成立：

> **新接入一个 OpenAI 兼容的厂商，应当是 0 行代码。**
> 新接入一个有自有协议的厂商，应当只写它与规范表示之间的差异部分。
> 新增一个模型，应当只是一条数据记录。

如果做不到，说明抽象层没找对位置。

---

## 2. 参考实现分析

两个项目要分开看，它们解决的不是同一个问题。

### 2.1 new-api：多厂商聚合，主要参考对象

`relay/channel/` 下有 **41 个厂商适配器**，规模上正好对应我们的目标。

**核心机制一：`Adaptor` 统一接口**

```go
type Adaptor interface {
    Init(info *RelayInfo)
    GetRequestURL(info *RelayInfo) (string, error)
    SetupRequestHeader(c *gin.Context, req *http.Header, info *RelayInfo) error
    ConvertOpenAIRequest(...)      ConvertClaudeRequest(...)
    ConvertGeminiRequest(...)      ConvertEmbeddingRequest(...)
    ConvertRerankRequest(...)      ConvertAudioRequest(...)
    ConvertImageRequest(...)       ConvertOpenAIResponsesRequest(...)
    DoRequest(...)                 DoResponse(...)
    GetModelList() []string        GetChannelName() string
}
```

**值得学的**：`DoRequest` / `DoResponse` 分离，请求构造与响应解析各自独立；`GetRequestURL` / `SetupRequestHeader` 把 URL 拼装和鉴权头分开，因为不同厂商这两处的差异是独立的。

**不该学的**：这是个 **14 方法的胖接口**。一个只做 chat 的厂商也被迫实现 `ConvertAudioRequest`、`ConvertImageRequest`、`ConvertRerankRequest`，实际写法是 `return nil, errors.New("not implemented")`。后果有三：新增一种能力要改所有 41 个适配器；编译期无法知道某厂商支持什么；"不支持"和"忘了实现"在类型上无法区分。这是接口隔离原则的教科书级反例，我们要按能力切分（§4.2）。

**核心机制二：`RelayFormat` × `RelayMode` 正交分解**

```
RelayFormat（入口协议）: openai | claude | gemini | openai_responses | rerank | embedding | ...
RelayMode（能力类型）  : ChatCompletions | Embeddings | ImagesGenerations |
                        AudioSpeech | AudioTranscription | Rerank | VideoSubmit | ...
```

这个分解是对的，我们直接沿用。但 new-api 的 `RelayMode` 里塞进了 **12 个 Midjourney 专有操作**（`RelayModeMidjourneyImagine`、`RelayModeMidjourneyBlend`……）。单个厂商的内部操作污染了顶层枚举——加一个新的图像厂商就得再加十几个枚举值。**厂商特有的子操作应当封装在该厂商的适配器内部，不该出现在顶层。**

**核心机制三：三个模型名分离**（最有价值的一条）

```go
OriginModelName   // 客户端请求里写的
BillingModelName  // 计费用的身份
UpstreamModelName // 实际发给上游的
```

源码注释写得很明白：*"virtual pricing aliases never participate in channel selection or upstream routing"*。这解决了一类真实问题：想给某个模型做一个折扣别名 `gpt-5.6-sol-promo`，它计费不同但路由到同一上游。如果只有一个模型名字段，这个需求会逼着你在渠道表里造假数据。**我们直接采纳，并再拆一个 `RequestedModelName` 用于日志与配额（见 §5.3）。**

**核心机制四：`Ability` 倒排索引**

```go
type Ability struct {
    Group     string  // 联合主键
    Model     string  // 联合主键
    ChannelId int     // 联合主键
    Enabled   bool
    Priority  *int64
    Weight    uint
}
```

由 `Channel.Models`（逗号分隔字符串）× `Channel.Group`（逗号分隔）做**笛卡尔积展开**生成。从模型反查可用渠道是 O(log n) 的索引查询——思路正确。

**但实现有两个问题**：其一，`Models` 存逗号分隔字符串是反范式，改任何一个模型都要 `DELETE + 重建`该渠道的全部 ability 行；其二，笛卡尔积在我们的规模下会爆炸——40 渠道 × 1000 模型 × N 分组，最坏几十万行，且每次渠道编辑都要重写其中一大片。**我们保留倒排索引的思路，但用正规化关联表 + 增量维护（§5.2）。**

**核心机制五：RelayKit —— 协议转换独立成模块**

这是 new-api 最好的一处设计。`relaykit/` 是一个**独立的 Go module**，有自己的 `go.mod`，README 里明确写着：

> 它只负责协议层的数据建模与语义转换，不包含 HTTP 服务、上游请求发送、渠道调度、鉴权、计费或数据库逻辑。

它支持 4 种文本协议两两互转，并且——这一点尤其值得称道——**为每条转换路径标注质量等级**：

| 源 \ 目标 | OpenAI Chat | OpenAI Responses | Claude Messages | Gemini |
|---|---|---|---|---|
| OpenAI Chat | — | Good | Fair | Fair |
| OpenAI Responses | Good | — | Fair | Fair |
| Claude Messages | Fair | Fair | — | **Discouraged** |
| Gemini | Fair | Fair | **Discouraged** | — |

`TextConverterQuality` 有三档：`good` / `fair` / `discouraged`。**承认协议之间语义并不对等，且把这个事实暴露给调用方**，而不是假装所有转换都无损。它还返回转换器 ID、实际经过的跳数和统一 usage，便于审计。代码里有 `tool_loss_policy_test.go`，说明工具调用在跨协议转换中丢失是被正面处理过的问题。

**这三点我们全部采纳**：协议转换独立成包、质量分级公开、转换过程可审计。

**反面教材：`RelayInfo` 上帝对象**

单个结构体 60+ 字段，混装了认证（`UserId`/`TokenKey`）、计费（`Billing`/`FinalPreConsumedQuota`）、流式状态（`IsStream`/`FirstResponseTime`）、协议转换的中间状态（`ClaudeToChatStreamState`）、WebSocket 连接（`ClientWs`/`TargetWs`）。源码注释暴露了代价：

> *"InitChannelMeta nils them so a retry cannot resume a dirty converter"*

——重试时必须手工把转换器状态置空，否则会复用脏状态。这是把可变状态塞进共享上下文的典型后果。**我们按生命周期把上下文拆开（§5.1）。**

### 2.2 sub2api：订阅额度分发，参考其运维侧设计

它解决的是另一个问题（把订阅账号转成 API 分发），且 README 首屏就有服务条款风险声明。协议适配层参考价值有限，但**渠道健康管理**这块做得比 new-api 细：

```
channel_monitor_checker.go          主动健康探针
channel_monitor_quota_fetcher.go    抓取上游剩余配额
channel_monitor_probe_retirement.go 探针退休（连续失败后降低探测频率）
channel_monitor_aggregator.go       健康数据聚合
channel_monitor_dailyrollup         日滚动汇总
```

其中**探针退休**值得单独说：一个渠道已经挂了，还按原频率去探测只是在浪费配额和产生噪声告警。退避式探测是对的。它的 ent schema 里还有两个概念我们要用：`compositemodelroute`（复合模型路由）和 `errorpassthroughrule`（错误透传规则）——后者对应"上游的错误该原样透传还是翻译成我们的错误码"这个真实问题。

它用 **ent** 而非 sqlc。ent 的 schema-as-code 在这类关系密集的场景确实舒服，但生成代码庞大，且我们的分析类查询要落到原生 SQL。`ARCHITECTURE.md` §2.2 的 sqlc 选择维持不变。

### 2.3 freellmapi：34 家供应商 / 635 端点，抽象做得最彻底

TypeScript 实现（627 个 TS 文件），定位是"聚合 34 家免费层供应商到一个 OpenAI 兼容端点"。规模与我们的目标最接近，且在三个项目里**抽象层做得最干净**。

**实证一：34 家供应商只用了 12 个文件、3863 行**

```
providers/
├── base.ts            474 行   BaseProvider 抽象类
├── openai-compat.ts   524 行   通用驱动，服务绝大多数供应商
├── google.ts          877 行   Gemini 协议
├── sail.ts            402 行
├── aihorde.ts         222 行
├── cloudflare.ts      214 行
├── modelscope.ts      153 行
├── cohere.ts          148 行
├── zhipu.ts           109 行
├── electronhub.ts      73 行
├── pollinations.ts     71 行
└── experiential.ts     36 行  ← 最小适配器
```

对照 new-api 的 41 个厂商 = 41 个包，这是同一问题的两种解法，**优劣一目了然**。这直接验证了 §4.4 的论点：厂商数量与代码量不该是线性关系。

`BaseProvider` 只有 **3 个抽象方法**（`chatCompletion` / `streamChatCompletion` / `validateKey`），对比 new-api 的 14 方法胖接口。

**最小适配器长什么样**（`experiential.ts` 全文 36 行的核心）：

```ts
export class ExperientialProvider extends OpenAICompatProvider {
  constructor() {
    super({ platform: 'experiential', name: 'Experiential Labs',
            baseUrl: 'https://api.experientiallabs.ai/v1' });
  }
  // 唯一的差异：某些推理路由固定 temperature=1，且不接受 top_p
  protected override samplingForModel(modelId, options) { … }
}
```

一个新供应商 = 构造函数三行 + 覆写它实际偏离标准的那一个方法。**这正是 §1 成本标尺想要的形态。**

**实证二：注释记录实证与日期，对付上游行为漂移**

```ts
// Verified against /api/models on 2026-09-06; Opus 5 also returned a live 400
// for temperature=0.7. These reasoning routes fix temperature at 1.
```

上游会悄悄改行为，而 quirks 是靠观测得来的。把"什么时候、怎么验证的"写进代码，比单纯写"这个模型不支持 temperature"有用得多。**我们的 `Quirks` 声明应当沿用这个规范。**

**实证三：key 状态必须三分，不是二分**

`pollinations.ts` 覆写 `validateKey` 的注释是教科书级的。默认探针用 `GET /v1/models`，但该供应商对**已吊销的 key 也返回 200**，导致死 key 被报成健康、路由静默降级（引用了 issue #608）。改用 `GET /account/key` 后，逐个状态码的处置是：

| 上游响应 | 含义 | 处置 |
|---|---|---|
| 200 | 认证通过 | key 存活 |
| 401 | 吊销/无效 | 计入连续失败，自动禁用 |
| **402** | 认证通过但额度耗尽 | **凭证没问题**，交给路由的配额路径处理，不动 key |
| **403** | 权限不足（可能是受限但有效的 key） | **结论不确定**，抛异常让健康检查保留上次判定，绝不禁用 |

**"key 无效" ≠ "key 没额度" ≠ "探测不确定"**——三者处置完全不同。前两个项目都只做了二分（健康/不健康）。把 402 当失败会误杀好凭证，把 403 当失败会因为一次权限问题禁用一个正常 key。

**实证四：流式超时有两个维度，不是一个**

```ts
export interface SseStreamOptions {
  firstByteTimeoutMs?: number;  // 首字节超时
  stallTimeoutMs?: number;      // 流中途停顿超时
}
export interface ProviderFetchOptions {
  timeoutBounds?: 'headers' | 'request';  // 超时算到响应头还是整个请求
}
```

`ARCHITECTURE.md` §6.2 与本文 §7 只提了首字节超时，**漏了 stall**：流已经开始、但上游中途卡住不再吐字节。这种情况下首字节超时早已失效，没有 stall 超时就会一直挂到客户端放弃。**这是我们方案的一个缺口，已补入 §7。**

**实证五：签名目录 feed —— 回答"1000+ 模型的元数据从哪来"**

`catalog-sync.ts` 的机制：

- 每日两次（或按需）从目录服务拉取模型目录，**含 quirks**
- **Ed25519 验签**，针对收到的确切字节；未签名或被篡改一律丢弃
- 分层：付费许可证拿 live 目录（2–3 天刷新），免费安装拿月度快照
- **版本单调性**：内置迁移是基线，只有拉取到的目录版本 **高于** 二进制自带版本才应用——防止过期快照回滚掉新版本已添加的模型

验签这一步不是过度设计。**目录里包含 quirks 与路由参数**，一旦 CDN 被攻破或遭中间人，攻击者可以注入模型、改写 quirks，从而操纵路由把流量导向自己控制的端点。目录是可执行的配置，必须当代码来保护。

**实证六：chat 模型与媒体模型分表**

```ts
// Generative-media modalities are routed into the separate media_models table,
// never into the chat `models` table.
const MEDIA_MODALITIES = new Set(['image', 'audio']);
```

因为两者的元数据结构差异过大——chat 关心 `context_length` / `tools` / `json_mode`，图像关心分辨率、步数、尺寸档位，音频关心采样率、声音。塞进一张表意味着大半字段恒为 null。**我们 §5.2 原本设计的单张 `models` 表要拆（已修正）。**

**值得商榷的地方**：`router.ts` 2256 行、`lib/fallback-loop.ts` 1260 行，单文件过大，职责边界模糊。另外它是 Node 单线程，高并发流式转发的性能特征与 Go 差别很大，其并发相关的取舍不能直接照搬。

### 2.4 三个项目的横向对比

| 维度 | new-api | sub2api | **freellmapi** | 本方案取舍 |
|---|---|---|---|---|
| 厂商数 : 适配器代码 | 41 : 41 个包 | — | **34 : 12 文件 3863 行** | 取 freellmapi |
| 适配器接口 | 14 方法胖接口 | — | **3 个抽象方法** | 取 freellmapi，再按能力切分 |
| 协议转换 | **RelayKit 独立模块 + 质量分级** | — | 散在 `*-map.ts` | 取 new-api |
| 渠道选择 | priority + weight 随机 | 健康优先 | **bandit 凸组合 + Thompson** | 取 freellmapi |
| 健康管理 | 冷却计时 | **探针退休 + 日滚动** | 冷却探测 + 连续失败禁用 | 取 sub2api + freellmapi |
| 额度追踪 | 配额扣减 | 上游配额抓取 | **月预算 + rpd/tpd/rpm/tpm 双形态** | 取 freellmapi |
| 模型元数据来源 | 手工 + 上游同步 | 手工 | **签名目录 feed** | 取 freellmapi |
| 模型名分离 | **三名分离** | — | — | 取 new-api，扩到四名 |
| 反面教材 | `RelayInfo` 上帝对象 | — | 单文件过大 | 均规避 |

---

## 3. 总体设计：四层解耦

把五维笛卡尔积拆成加法，靠的是在正确的位置切四刀：

```
┌──────────────────────────────────────────────────────────────┐
│ L1  入口协议层  Ingress Protocol                              │
│     OpenAI Chat / Responses / Anthropic / Gemini             │
│     职责：解析入口格式 → 规范请求；规范响应 → 出口格式         │
└────────────────────────┬─────────────────────────────────────┘
                         │  Canonical Request / Response / Event
┌────────────────────────▼─────────────────────────────────────┐
│ L2  编排层  Orchestration                                     │
│     能力协商 · 模型解析 · 渠道选择 · 回退 · 计量 · 审计         │
│     职责：决定这个请求由谁来做、失败了怎么办、算多少钱          │
│     ★ 完全不认识任何厂商，也不认识任何入口协议                  │
└────────────────────────┬─────────────────────────────────────┘
                         │  Canonical + RouteDecision
┌────────────────────────▼─────────────────────────────────────┐
│ L3  厂商协议层  Provider Protocol                             │
│     规范表示 ↔ 厂商线格式                                     │
│     openai_compatible（默认）/ anthropic / gemini / bedrock…  │
└────────────────────────┬─────────────────────────────────────┘
                         │  *http.Request / SSE stream
┌────────────────────────▼─────────────────────────────────────┐
│ L4  传输层  Transport                                         │
│     鉴权注入 · URL 拼装 · 超时 · 重试 · 连接池 · 流式管道       │
└──────────────────────────────────────────────────────────────┘
```

**为什么是这四刀，而不是 new-api 的"一个 Adaptor 管到底"**：

新增一个厂商时，实际变化的东西通常只落在 L3 或 L4 的一小块——多数厂商是"OpenAI 兼容但 base_url 和鉴权头不同"，那只动 L4 的配置；少数是"协议不同"，那只写 L3 的一个转换器。把 L1、L2 隔离开，它们对新厂商完全无感。

而 new-api 的 `Adaptor` 把 L3 和 L4 焊在一起（`GetRequestURL`、`SetupRequestHeader`、`ConvertXxxRequest`、`DoRequest` 在同一个接口里），于是每个 OpenAI 兼容厂商都得复制一遍相同的转换逻辑，只为了改一个 URL。它的 41 个适配器里有相当一部分是这种重复。

---

## 4. L1 与 L3：协议转换

### 4.1 规范表示（Canonical IR）与转换拓扑

**关键决策：不把 OpenAI 格式当作中间表示。**

隐式以 OpenAI 为中枢是最常见的做法（也是 new-api 的实际形态），代价是 **Claude → Gemini 这类请求会被降级两次**：Claude 独有的语义先在转 OpenAI 时丢一轮，再在 OpenAI 转 Gemini 时丢一轮。RelayKit 把 Claude↔Gemini 标为 `Discouraged` 正是这个原因。

我们定义独立的规范表示 `ir.Request` / `ir.Response` / `ir.StreamEvent`，它是四种协议语义的**并集**而非交集——某个协议独有的字段在 IR 里有位置，只是转到不支持它的目标协议时才丢弃，并显式记录损失。

但纯 IR 中枢有个问题：高频路径（OpenAI↔OpenAI 兼容，占实际流量的绝大多数）没必要绕一圈。所以采用**混合拓扑**：

```
       直接转换器（高频路径，无损或近无损）
    ┌─────────────────────────────────────┐
    │                                     ▼
OpenAI Chat ◄──────► IR ◄──────► Anthropic Messages
    ▲                 ▲                   ▲
    │                 │                   │
    └── Responses ────┴──── Gemini ───────┘
              经 IR 中转（兜底，覆盖所有组合）
```

- **注册直接转换器**：为语义接近的高频对写专用转换器（OpenAI Chat ↔ OpenAI 兼容上游，本质是恒等变换；OpenAI Chat ↔ Responses）。
- **IR 兜底**：任意两个协议之间一定有路径可走（源 → IR → 目标），不会出现"不支持这个组合"。
- **路径选择**：注册表按 `(from, to)` 查找，有直接转换器就用，否则走 IR，返回实际路径与质量等级。

对比 new-api：它注册了 12 个直接转换器覆盖 4×3 个有向对，没有 IR 兜底。加第 5 个协议时要新写 8 个转换器（4×2）；我们的方案只需要写 2 个（协议↔IR）。**这就是把乘法拆成加法的具体体现。**

### 4.2 转换质量与损失策略

沿用 RelayKit 的质量分级，但把它从"文档里的表格"变成**运行时的一等公民**：

```go
type ConversionQuality string
const (
    QualityLossless     ConversionQuality = "lossless"     // 恒等或完全同构
    QualityGood         ConversionQuality = "good"         // 核心结构对齐
    QualityFair         ConversionQuality = "fair"         // 主要能力可转，部分特性降级
    QualityDiscouraged  ConversionQuality = "discouraged"  // 语义差异大，建议避免
)

type ConversionResult[T any] struct {
    Value    T
    Path     []string          // 实际经过的转换器 ID，如 ["claude→ir","ir→gemini"]
    Quality  ConversionQuality // 全路径取最低档
    Losses   []Loss            // 具体丢了什么
}

type Loss struct {
    Field  string   // "tools[2].strict" / "reasoning_effort"
    Reason string   // "target protocol has no equivalent"
    Policy LossPolicy
}
```

`Loss` 的处理策略可配置，这一点比 new-api 前进一步——它有 `tool_loss_policy` 但只针对工具：

| 策略 | 行为 | 适用 |
|---|---|---|
| `reject` | 直接 422，告知客户端该组合不支持此特性 | 严格模式；金融等不容降级的场景 |
| `drop` | 静默丢弃，记进 `Losses` 与请求日志 | 默认；特性是可选增强时 |
| `emulate` | 用目标协议的其他机制模拟 | 如 JSON mode 用 prompt 约束模拟 |
| `passthrough` | 塞进目标协议的扩展字段 | 目标支持透传时 |

**响应头回传质量信息**，让客户端有知情权：

```
X-UMaaS-Conversion-Path: claude→ir→gemini
X-UMaaS-Conversion-Quality: fair
X-UMaaS-Conversion-Losses: tools[2].strict,reasoning_effort
```

这条在排查"为什么换了个模型效果就变差"时价值极大——那往往不是模型变差了，是某个参数在转换中被丢了。

### 4.3 流式事件的统一

这是全方案最难的一块，因为三种协议的流式语义结构差异巨大：

| 协议 | 流式结构 |
|---|---|
| OpenAI Chat | 扁平 `choices[].delta`，工具调用靠 `index` 累积拼接 |
| Anthropic | 显式块生命周期：`message_start` → `content_block_start` → `content_block_delta` → `content_block_stop` → `message_delta` → `message_stop` |
| Gemini | `candidates[].content.parts[]`，每个 chunk 是完整快照片段而非增量 |

**统一为规范事件流**，取三者的并集语义（块生命周期显式化，因为从"有块结构"降级到"无块结构"容易，反向很难）：

```go
type StreamEvent interface{ isStreamEvent() }

type EventMessageStart  struct{ ID, Model string; Role string }
type EventBlockStart    struct{ Index int; Kind BlockKind }  // text|tool_use|thinking|image
type EventBlockDelta    struct{ Index int; Text string; PartialJSON string }
type EventBlockStop     struct{ Index int }
type EventMessageDelta  struct{ StopReason string; Usage *Usage }
type EventMessageStop   struct{}
type EventError         struct{ Err *ProviderError }   // 流中途失败，据实上报
```

三个必须处理好的细节：

1. **工具调用的增量拼接**。OpenAI 把工具参数 JSON 切成任意片段跨 chunk 传输，Anthropic 用 `input_json_delta`。规范层维护每个块的 JSON 累积缓冲，在 `BlockStop` 时才做完整性校验。**不能边收边解析 JSON**——中间态必然是非法 JSON。

2. **Gemini 的快照语义**。它的 chunk 不是纯增量，转规范事件时要与上一帧做 diff 才能得到真正的 delta。这是 `Gemini → 其他` 被标为 Fair 的主因。

3. **usage 的到达时机**。OpenAI 在最后一个 chunk（需 `stream_options.include_usage`），Anthropic 分散在 `message_start`（input）和 `message_delta`（output），Gemini 每帧都带累积值。规范层统一在 `EventMessageDelta` 汇总，计费只认这一处（§5.4）。

### 4.4 厂商协议只有五类，不是四十类

这是让"40+ 厂商"退化为"5 个协议 + 40 条配置"的关键观察。实际统计 new-api 的 41 个适配器：

| 上游协议族 | 厂商 | 我们的实现 |
|---|---|---|
| **OpenAI 兼容** | DeepSeek、Moonshot、Mistral、xAI、SiliconFlow、Ollama、Together、Groq、01.AI、Perplexity、OpenRouter、vLLM、SGLang、Xinference… **约 30 家** | **零代码，纯配置** |
| **Anthropic** | Anthropic、Bedrock-Claude、Vertex-Claude | 1 个转换器 + 3 条鉴权配置 |
| **Gemini** | Google AI Studio、Vertex AI | 1 个转换器 + 2 条鉴权配置 |
| **自有协议** | 百度千帆、讯飞星火、腾讯混元、Coze、Dify | 各 1 个转换器 |
| **异步任务** | Midjourney、可灵、即梦、Replicate | 走 TaskProvider（§6） |

**约七成的厂商是 OpenAI 兼容的**，它们之间的差异全部落在 L4 传输层——base_url、鉴权头形态、少数字段的支持与否。这些应当是**数据库里的一行配置**，而不是一个 Go 文件。

new-api 为这些厂商各写了一个包（`deepseek/`、`moonshot/`、`mistral/`、`xai/`…），内容高度重复。我们用一个 `openai_compatible` 驱动 + 配置描述：

```go
type ProviderProfile struct {
    Protocol      string   // "openai_compatible" | "anthropic" | "gemini" | ...
    BaseURL       string
    AuthScheme    AuthScheme  // bearer | header | query | aws_sigv4 | gcp_oauth
    AuthHeader    string      // "Authorization" | "x-api-key" | "api-key"
    AuthPrefix    string      // "Bearer " | ""
    PathTemplate  map[Capability]string  // chat: "/v1/chat/completions"
    Quirks        Quirks
}

// Quirks：与标准的偏离项。绝大多数厂商只需要设一两个
type Quirks struct {
    NoStreamUsage       bool // 不支持 stream_options.include_usage
    NoSystemRole        bool // 不认 system role，需并入首条 user
    MaxTokensRequired   bool // max_tokens 必填
    ToolChoiceUnsupported bool
    JSONModeVia         string // "response_format" | "prompt" | ""
    ...
}
```

新增一个 OpenAI 兼容厂商 = 插一行 `provider_profiles` 记录。**这是 §1 那个成本标尺能否达标的关键。**

---

## 5. L2 编排层

编排层是整套设计里唯一"知道全局"的地方，也是唯一不该知道任何厂商细节的地方。

### 5.1 上下文拆分（对 `RelayInfo` 的修正）

new-api 的教训是把所有状态塞一个结构体。我们按**生命周期与可变性**切三份：

```go
// 请求级不可变。构造后只读，可安全并发传递
type RequestContext struct {
    RequestID   string
    Ingress     IngressFormat   // 入口协议
    Capability  Capability      // 能力类型
    Principal   Principal       // 谁在调用（user/workspace/api_key）
    Models      ModelNames      // 见 §5.3
    ReceivedAt  time.Time
}

// 尝试级可变。每次换渠道重试都新建一个，杜绝脏状态复用
type Attempt struct {
    Index       int
    Channel     *Channel
    Upstream    UpstreamModel
    Converter   *stream.Converter  // 流式转换器，每次尝试独立
    StartedAt   time.Time
    FirstByteAt time.Time
}

// 计费会话，独立生命周期（可能跨越多次 attempt）
type BillingSession interface {
    PreConsume(ctx, estimate Quota) error
    Settle(ctx, actual Usage) error
    Refund(ctx, reason string) error
}
```

**每次重试新建 `Attempt`** 从结构上消除了 new-api 那个"必须手工置空转换器状态"的坑——脏状态没有存活的地方。

### 5.2 模型注册表与路由索引

保留 `Ability` 倒排索引的思路，修掉反范式与笛卡尔积重建的问题。

```sql
-- 模型元数据。按模态分表：chat 与生成式媒体的元数据结构差异过大，
-- 合表意味着大半字段恒为 null（freellmapi 的 media_models 分表印证了这点）
models(                        -- 文本/对话模型
  id, provider_slug, slug, display_name,
  context_length, max_output_tokens,
  capabilities jsonb,          -- 见下方 Capability Manifest
  status, released_at
)
media_models(                  -- 图像/音频/视频生成模型
  id, provider_slug, slug, display_name,
  modality,                    -- image | audio | video
  spec jsonb,                  -- 分辨率档位、时长、采样率、声音列表…
  status, released_at
)

-- 渠道：40+ 行
channels(id, provider_profile_id, name, base_url_override, credentials_enc,
         region, priority, weight, status, health_state)

-- 路由索引：正规化关联表，取代逗号分隔字符串
channel_models(
  channel_id, model_id,
  upstream_model_name,     -- 上游实际叫什么，取代 ModelMapping JSON
  enabled, priority_override, weight_override,
  PRIMARY KEY (channel_id, model_id)
)

-- 分组可见性：与路由解耦，不参与笛卡尔积
group_models(group_id, model_id, PRIMARY KEY (group_id, model_id))
```

**与 new-api `Ability` 的差别**：

| | new-api | 本方案 |
|---|---|---|
| 模型列表存储 | `Channel.Models` 逗号分隔字符串 | `channel_models` 关联表 |
| 分组 | 参与笛卡尔积，行数 ×N | 独立表，不参与相乘 |
| 编辑一个模型 | 删除并重建该渠道全部 ability 行 | UPDATE 一行 |
| 行数量级（40渠道×1000模型×5分组） | 最坏 20 万 | 4 万（且不随分组增长） |
| 上游模型名映射 | `ModelMapping` JSON 字段解析 | 关联表上的列，可索引 |

选渠道的查询走 `(model_id, enabled, priority DESC, weight)` 复合索引，热路径再叠一层内存缓存（渠道配置是慢变数据，秒级 TTL 足够）。

### 5.3 四个模型名

new-api 分了三个，我们再拆一个。这不是过度设计——每个名字对应一个真实且不同的用途，合并任意两个都会在某个场景出错：

```go
type ModelNames struct {
    Requested string  // 客户端写的原文，如 "gpt-5.6-sol"
                      // 用途：日志、错误消息、响应体里的 model 字段回显
    Resolved  string  // 解析别名/版本后的规范 ID，如 "openai/gpt-5.6-sol-2026-08"
                      // 用途：渠道选择、能力协商
    Billing   string  // 计费身份，可以是虚拟别名
                      // 用途：仅计价。绝不参与路由
    Upstream  string  // 上游实际接受的名字，如 "gpt-5.6-sol"
                      // 用途：仅发给上游
}
```

一个具体例子说明为什么不能合并：促销别名 `gpt-5.6-sol-promo` 的 `Billing` 是它自己（打折价），`Resolved` 和 `Upstream` 都是真实模型。若用同一个字段，要么路由找不到渠道，要么计费算成原价。

### 5.4 能力协商（Capability Manifest）

这是"1000+ 模型"真正的管理手段。每个模型声明自己支持什么：

```jsonc
{
  "streaming": true,
  "tools": { "supported": true, "parallel": true, "strict_schema": false },
  "json_mode": "native",          // native | prompt_only | none
  "vision": { "supported": true, "max_images": 20, "formats": ["png","jpeg","webp"] },
  "audio_in": false,
  "reasoning": { "supported": true, "controllable_effort": true },
  "system_role": true,
  "temperature_range": [0.0, 2.0],
  "max_output_tokens": 32768
}
```

请求进来后做一次**协商**：请求所需能力 ∩ 模型声明能力 ∩ 目标协议能力。三者的差集就是要处理的损失，按 §4.2 的策略走。

这解决了一个具体的痛点：用户把请求从模型 A 换到模型 B，A 支持工具调用而 B 不支持。**没有协商层就是上游返回一个含义不明的 400**；有协商层则可以在发出请求前给出准确错误，或按策略降级并在响应头说明。

Manifest 的维护：优先从上游 `/models` 接口同步（`ARCHITECTURE.md` 提到的上游同步差异审阅正是入口），无法自动获取的字段人工标注。**1000 个模型不可能全靠手填**，所以能力探测要能半自动——用固定探针请求实测（这一点借鉴 sub2api 的 `channel_monitor_checker`）。

### 5.5 渠道选择：凸组合评分 + 乘性护栏

原本这里写的是 new-api 式的 `priority ASC + weight 加权随机`。读完 freellmapi 的 `scoring.ts` 后**重写**——那个二维方案在 40 渠道规模下不够用。

```
候选集 = channel_models(model_id) ∩ 健康渠道 ∩ 分组可见 ∩ 区域约束 ∩ 能力满足

base      = w_rel·reliability + w_speed·speed + w_intel·intelligence   // 凸组合，∈[0,1]
effective = base × headroomFactor × rateLimitFactor                    // 乘性护栏
```

**为什么是凸组合而不是"加一堆 bonus"**。freellmapi 的注释记录了它重构前的形态：*"summing a pile of hand-tuned, dimensionally-incompatible bonuses (a probability + a raw latency term + an intelligence term, each hand-capped to keep orderings sane)"*——把成功率（概率）、延迟（毫秒）、智能分（任意刻度）直接相加是**量纲错误**，只能靠手工封顶维持排序合理，最终不可维护。

归一化到 [0,1] 后取凸组合（权重和为 1），结果天然有界、可解释，且不需要任何人工封顶。

**策略即权重向量，引擎唯一**：

| 策略 | reliability | speed | intelligence |
|---|---|---|---|
| `balanced`（默认） | 0.50 | 0.25 | 0.25 |
| `smartest` | 0.35 | 0.10 | 0.55 |
| `fastest` | 0.35 | 0.55 | 0.10 |
| `reliable` | 0.70 | 0.15 | 0.15 |
| `custom` | 用户自定义 | | |
| `priority` | 退回手工链，不走评分 | | |

注意 `smartest` 的 reliability 仍有 0.35、`fastest` 仍有 0.35——**一个聪明但总是失败的模型不该赢，一个快但坏掉的模型也不该赢**。权重设计本身编码了这个判断。

**护栏是乘性的，这一点很关键**：

```
headroomFactor  → 接近免费额度上限的渠道被压低
rateLimitFactor → 正在返回 429 的渠道被压低
```

乘性因子**不改变健康渠道之间的相对排序**，只在某个渠道变危险时把它单独拉下来。若改成加性惩罚，会连带扰动所有候选的相对次序。

`headroomRamp` 的形状（两个护栏共用同一函数，防止两处阈值漂移）：

```
factor = 1                                    当 remaining ≥ rampStart
       = floor + (1-floor)·(remaining/rampStart)   否则线性下降
```

**"未知即不表态"**：额度未知时返回 1 而非 0。缺数据不该被当成坏消息——这是很容易写错的地方，一个新接入、还没有统计数据的渠道不应该因此被判死刑。

**可靠性用 Beta 后验 + Thompson 采样，而不是确定性成功率。** 确定性评分有个硬伤：模型失败几次后分数变低，就再也不会被选中，于是**永远没机会证明自己已经恢复**。Thompson 采样让探索强度与不确定性成正比，自动实现"偶尔试一下"，无需显式的冷却计时器与解冻阈值。

**模型排序与凭证选择是两个正交决策**。freellmapi 特意没把 `KeySelectionStrategy` 并入 `RoutingStrategy`，理由是*"folding this into the strategy enum would mean switching key policy also switches (or disables) the model bandit"*。同一渠道下有多个 key 时：

- `auto`：按 key 维度的 bandit 分，无数据时轮询
- `least-remaining`：优先用剩余额度最少的 key。**对免费额度聚合这是对的**——额度按周期重置，用不完就浪费了

**回退链**：按 effective 分数降序，最多尝试 N 个。

> **自建 GPU 供给接入本评分引擎的方式**见 [`SELF-HOSTED-GPU-OPERATIONS.md`](./SELF-HOSTED-GPU-OPERATIONS.md) §2。
> 供给实际分三层（自有机房 / 云上 GPU 实例 / 云端 API），`costFactor` 相应分档；
> 队列深度并入 `speed` 因子、`headroomFactor` 对自建喂显存水位而非配额。
> 三者叠加使「自有机房优先 → 云上实例 → 云端 API 溢出」成为评分的**涌现结果**。
>
> **但有一类约束不能用评分表达**：数据驻留（某些请求不得离开内网）是零容忍的合规要求，
> 必须在评分**之前**做可行集硬过滤，且过滤后为空时明确报错而非降级。见该文 §2.1。

**回退的边界条件比排序重要**：

| 情况 | 可否换渠道重试 |
|---|---|
| 连接失败 / DNS / 超时（未发出） | ✅ 可以 |
| 上游 5xx | ✅ 可以 |
| 上游 429 | ✅ 换渠道，且给当前渠道降权 |
| 上游 4xx（参数错误） | ❌ 换了也一样错，直接返回 |
| **流式已发出首个 chunk 后失败** | ❌ **绝对不可以**（见 §7） |

健康状态机借鉴 sub2api，并加入**探针退休**：连续失败的渠道降低探测频率，避免对已死的上游持续打无效请求。

---

## 6. 异步任务（视频/图像生成）

video、Midjourney 这类能力不是"慢的 chat"，而是**另一种交互形态**：提交拿任务 ID → 轮询 → 取结果。硬塞进同步接口会把整个链路的超时设计带偏。

new-api 用独立的 `TaskAdaptor` 接口是对的，我们沿用并简化。它的**三阶段计费**设计尤其值得完整照搬：

```go
type TaskProvider interface {
    Submit(ctx, *ir.TaskRequest) (*TaskHandle, error)
    Poll(ctx, *TaskHandle) (*TaskStatus, error)
    Fetch(ctx, *TaskHandle) (*TaskResult, error)

    // 三阶段计费
    EstimateCost(*ir.TaskRequest) (Quota, error)        // 提交前预扣
    AdjustOnSubmit(*TaskHandle) (Quota, bool)           // 上游回参与预估不符时调整
    SettleOnComplete(*TaskStatus) (Quota, error)        // 终态结算，多退少补
}
```

为什么必须三阶段：视频生成按秒计费，而实际时长可能与请求参数不符（上游会裁剪或补齐）。只在提交时扣费会算错，只在完成时扣费则给了刷单空间——请求发出到完成之间有几分钟窗口。new-api 的 `ForcePreConsume` 注释说明了这一点：*"异步任务必须在提交前锁定全额"*。

---

## 7. 与数据平面的衔接：流式失败不可重试

`ARCHITECTURE.md` §6.2 已经提出这个约束，这里给出协议层的应对。

问题：回退链在非流式请求里很自然，但流式一旦发出首个 chunk，客户端已收到部分内容，此时上游断连**无法重试**——重试会导致内容重复或不连贯。

三层应对：

1. **两个独立的流式超时**（这一条原本漏了 stall，由 freellmapi 的 `SseStreamOptions` 补上）：

   | 超时 | 覆盖阶段 | 超时后 |
   |---|---|---|
   | `firstByteTimeout`（如 10s） | 发出请求 → 首个 chunk | **仍在安全窗口，换渠道重试** |
   | `stallTimeout`（如 30s） | 相邻两个 chunk 之间 | 已过安全窗口，只能中止并如实上报 |

   只设首字节超时是不够的：流已经开始、上游中途卡住不再吐字节时，首字节超时早已失效，没有 stall 超时就会一直挂到客户端自己放弃。另外 HTTP 客户端的超时边界要区分"到响应头为止"还是"整个请求"——流式必须用前者，否则整体超时会在正常的长响应中途把连接砍断。
2. **首字节前不向下游写任何东西**。哪怕收到了 HTTP 200 header，只要还没有实际内容，就仍在安全窗口内。**不要急着 flush 响应头。**
3. **首字节后的失败据实上报**，以 `EventError` 转成出口协议的错误事件，请求日志标 `partial_failure`，计费按实际产出 token 结算。

**明确禁止的做法**：中断时静默补一个 `finish_reason: "stop"` 假装正常结束。客户端会以为拿到了完整回答——这比明确报错有害得多。

---

## 8. 目录结构

```
backend/internal/
├── ir/                          规范表示（独立包，不依赖 HTTP/DB）
│   ├── request.go  response.go  stream.go  usage.go  capability.go
│
├── protocol/                    L1 + L3：协议转换
│   ├── registry.go              (from,to) → 转换器，含路径选择与质量
│   ├── quality.go               质量等级与损失策略
│   ├── openai/                  chat + responses，请求/响应/流
│   ├── anthropic/
│   ├── gemini/
│   └── convert_test.go          黄金用例：每对协议的往返测试
│
├── provider/                    L4：传输
│   ├── profile.go               ProviderProfile + Quirks
│   ├── driver.go                Driver 接口（按能力切分，见下）
│   ├── openaicompat/            一个驱动服务约 30 家厂商
│   ├── anthropic/  gemini/  bedrock/  vertex/
│   └── task/                    异步任务厂商
│
├── gateway/                     L2：编排
│   ├── negotiate.go             能力协商
│   ├── resolve.go               四个模型名的解析
│   ├── route.go                 渠道选择与回退
│   ├── attempt.go               单次尝试的生命周期
│   ├── stream.go                流式管道与首字节窗口
│   └── meter.go                 用量归集
```

**按能力切分驱动接口**，修正 new-api 胖接口的问题：

```go
type Driver interface {                       // 所有驱动都要有
    Profile() ProviderProfile
    BuildRequest(*Attempt, *ir.Request) (*http.Request, error)
}

// 以下按能力可选实现，用类型断言检测
type ChatDriver      interface{ Driver; ParseChat(*http.Response) (*ir.Response, error) }
type StreamDriver    interface{ Driver; ParseStream(io.Reader) iter.Seq2[ir.StreamEvent, error] }
type EmbeddingDriver interface{ Driver; ParseEmbedding(*http.Response) (*ir.Embeddings, error) }
type AudioDriver     interface{ Driver; ... }
type ImageDriver     interface{ Driver; ... }
type RerankDriver    interface{ Driver; ... }
type TaskDriver      interface{ Driver; TaskProvider }
```

收益：只做 chat 的厂商就实现两个方法；新增一种能力不需要改动任何现有驱动；**"不支持"与"没实现"在类型层面可区分**——能力协商阶段直接查驱动是否实现了对应接口，而不是调用后拿到 `not implemented` 错误。

---

## 9. 测试策略

协议转换是最容易出现"改 A 坏 B"的地方，测试必须结构化。

1. **黄金用例（golden files）**。每对协议的每种典型请求（纯文本、多模态、工具调用、流式、推理内容）固化输入输出。RelayKit 的 `golden_test.go` + `testdata/` 就是这个做法，直接借鉴。
2. **往返测试**。`A → IR → A` 必须等价（幂等性）；`A → IR → B → IR → A` 的损失必须等于声明的 `Losses`——**声明的损失和实际损失不一致就是 bug**。
3. **流式分片鲁棒性**。同一段 SSE 用不同的字节切分方式喂进解析器，结果必须一致。真实网络会在任意位置切断，这类 bug 在生产环境才暴露的代价很高。
4. **厂商契约测试**。对每个 `ProviderProfile` 跑一组固定探针请求（打标签，默认跳过，需要真实密钥时手动执行），验证 Quirks 声明与实际相符。上游是会悄悄改行为的。

---

## 10. 实施顺序

对应 `ARCHITECTURE.md` §9 的 B3–B4：

| 步骤 | 内容 | 判据 |
|---|---|---|
| **P1** | `ir/` 规范表示 + `protocol/openai`（chat，含流式） | OpenAI SDK 直连可用 |
| **P2** | `provider/openaicompat` + `ProviderProfile` 配置化 | **接入 DeepSeek / Moonshot / SiliconFlow 零代码** |
| **P3** | 能力协商 + 四模型名解析 + 渠道选择与回退 | 主渠道故障自动切换；不支持的特性给出准确错误 |
| **P4** | `protocol/anthropic` + 转换质量与损失策略 | Claude 格式入口可用；响应头回传质量 |
| **P5** | `protocol/gemini` + IR 兜底路径 | 任意两协议互通 |
| **P6** | Embedding / Rerank / Audio 驱动 | 多模态能力打通 |
| **P7** | `TaskProvider` + 三阶段计费 | 视频生成类可用 |

**P2 是关键验证点**。如果接入第三个 OpenAI 兼容厂商时还需要写 Go 代码，说明 §4.4 的抽象没做对，此时返工的成本远低于写到第 30 个厂商时。

---

## 11. 需要确认的问题

**A. 入口协议范围。** 首版是否需要同时提供 OpenAI、Anthropic、Gemini 三种入口？只做 OpenAI 兼容入口能省掉 §4 的大半工作量。考虑到 Claude Code 等 Agent 框架原生说 Anthropic 协议，**建议至少支持 OpenAI + Anthropic 两种入口**。

**B. 首版能力范围。** chat 是必须的；embedding / rerank / audio / image / video 各自都有独立的协议与计费模型。建议首版 chat + embedding，其余按需。

**C. 转换损失的默认策略。** 建议默认 `drop` + 响应头声明。若面向金融等严格场景，可能需要默认 `reject`。这个默认值影响用户对"换模型"的预期。

**D. 是否复用 RelayKit。** 它是 Apache 协议的独立 Go module，直接依赖可以省掉 §4 的大部分工作。代价是：接受它的 IR 设计（以四协议直转为主、无独立 IR 兜底），以及跟随其版本节奏。**建议先读其源码评估，而不是直接依赖**——协议转换是核心资产，受制于人的风险要算清楚。

**F. 是否自建签名目录服务。** freellmapi 把模型目录做成了产品的一部分：签名 feed、分层刷新、版本单调性。这解决了"1000+ 模型元数据如何持续维护"的问题，但需要额外运营一个目录服务与签名密钥。**若不自建，退化为随版本发布的静态数据 + 上游 `/models` 同步**，代价是新模型要等下一次发版。建议首版走后者，把目录表结构设计成可被 feed 覆盖的形态，留好升级路径。

**E. 模型能力 Manifest 的初始数据从哪来。** 1000+ 模型的能力矩阵人工填不现实。可选：厂商官方文档抓取、上游 `/models` 接口、社区数据集（如 OpenRouter 的模型元数据）、探针实测。建议组合使用并标注每个字段的来源与置信度。
