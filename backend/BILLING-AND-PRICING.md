# 计价与计费技术方案

> 状态：待评审 · 2026-09-08
> 归属：`ARCHITECTURE.md` §4.3「用量与计费」与 §6.3「计量与计费」的展开
> **本文是以下内容的真相源**：价目模型与版本化、计费口径（什么算钱、按谁的数算）、
> 预扣—结算—释放流程、余额与信用额度、成本归集与毛利、计费相关的对账。
> **本文受以下约束**：单一 PostgreSQL 生产库、Redis 为必需组件（`ARCHITECTURE.md` §2.5）、
> **计费不跨服务边界**（`SERVICE-DECOMPOSITION.md` §1）、四模型名分离（`UNIFIED-PROVIDER-INTERFACE.md` §5.3）、
> 自建供给的成本是摊销值（`SELF-HOSTED-GPU-OPERATIONS.md` §4）。

---

## 1. 为什么单独立这份文档

`SERVICE-DECOMPOSITION.md` §1 把「计费必须强一致」抬到了**否决服务拆分**的高度——它是整套架构里唯一一条能改变服务边界形状的业务约束。但在此之前，计费在四份文档里只有碎片：

| 出处 | 已有内容 | 缺口 |
|---|---|---|
| `ARCHITECTURE.md` §4.3 | `request_logs` / `quotas` 字段清单 | 价目表只是 `models` 表里的一个"定价"占位；`invoices/topups` 是字面的省略号 |
| `ARCHITECTURE.md` §6.3 | "配额扣减走 Redis 原子裁决" | 这是一致性策略，不是计费设计。预扣算多少、悬挂了谁来收，都没说 |
| `UNIFIED` §5.3 | 四模型名里的 `Billing` | 只说了"计费别名不参与路由"，反向的"路由不影响计费"没写 |
| `UNIFIED` §6 | 异步任务三阶段计费 | 是全文最完整的一段，但只覆盖异步任务 |
| `SELF-HOSTED` §4 | 自建摊销成本 = 利用率的函数 | 摊销值要回填，而回填的目标字段与用户账单共用，冲突未识别 |
| `SERVICE` §1 | 计费不跨服务边界 | 边界定了，边界内的东西没定 |

**一个能否决架构决策的域，却没有自己的真相源。** 这份文档补这个洞。

范围声明：本文只管**钱怎么算、怎么扣、怎么对平**。价格定多少是商业决策，不在此列；本文只保证任何定价策略都能被这套模型表达。

---

## 2. 第一条原则：成本与售价是两本账

这是本方案最重要的一条，其余大半设计都是它的推论。

现状是全系统只有一个 `cost_usd_micro`（`ARCHITECTURE.md` §4.3），它同时被要求承担两件性质完全不同的事：

| | 我们付出去的 | 我们收进来的 |
|---|---|---|
| 语义 | 上游 token 费 / 自建摊销 | 向用户计费的金额 |
| 可变性 | **可回填、可修正**（自建摊销当期结束才准） | **一经结算不可变**（用户已经付过了） |
| 归属 | 渠道 / 供给层 | 模型（`Billing` 名） |
| 用途 | 毛利分析、供给决策 | 账单、发票、余额 |

合成一个字段会同时踩三个坑：

1. **自建摊销回填会改写已出账单。** `SELF-HOSTED` §4 明确要求"后台任务按周期回填"，且"当期结束前是估算值"。若回填写的是同一个字段，用户上个月已经付过的账单会在月末被静默改掉。
2. **毛利算不出来。** `frontend/admin` 的 `BillingPayload.summary` 已经要 `mrr / outstanding / refunded / arpu`，没有售价字段就没有 MRR 的数据来源。
3. **重试成本无处安放。**（见 §4.3）

**结论**：`request_logs` 上必须是三个字段，不是一个。

```
upstream_cost_nano   bigint   -- 成本。可由后台任务回填修正
charged_amount_nano  bigint   -- 售价。结算后不可变
price_version_id     bigint   -- 按哪一版价目算的（见 §3.3）
```

由此还导出一条时序上的边界，文档此前没有识别到：

> **用户账单只依赖售价（请求发生时即确定），成本报表才依赖摊销（当期结束后才准）。**
> 两者的结账周期不同是正常的，不要试图对齐——对齐的唯一办法是延迟出账单，那是错的方向。

---

## 3. 价目模型

### 3.1 计价维度远不止两个

契约侧（`frontend/contracts/openapi.yaml` `ModelSummary.pricing`）目前只有 `input_per_million` / `output_per_million` 两个字段。而**你们自己的 `request_logs` 已经埋了这些维度**：

```
prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, tool_call_count
```

`cache_read` / `cache_write` / `tool_call` **没有任何价格可以对应**。埋点跑在价目表前面，说明价目表没被设计过。

实际需要表达的计价单元：

| 单元 | 说明 | 谁有 |
|---|---|---|
| `input` | 输入 token | 全部 |
| `output` | 输出 token | 全部 |
| `cache_read` | 命中缓存的输入 token，通常是 input 的 10% | OpenAI / Anthropic / DeepSeek… |
| `cache_write` | 写入缓存，通常高于 input | Anthropic **且 5m / 1h 两档不同价** |
| `reasoning` | 推理 token。部分厂商与 output 同价，部分单列 | 推理模型 |
| `input_long_context` | **超过阈值后的分档价**（Gemini 超长上下文单价翻倍） | Gemini 等 |
| `request_fixed` | 每请求固定费 | 少数厂商、部分图像模型 |
| `image_per_unit` | 按张，随分辨率/步数分档 | 图像模型 |
| `audio_per_second` | 按秒 | TTS / STT |
| `video_per_second` | 按秒，且实际时长可能与请求不符（见 §5.6） | 视频模型 |
| `rerank_per_doc` | 按文档数 | rerank |
| `batch_discount` | 批量折扣系数 | OpenAI batch 等 |

**这些不是"以后再加的列"，是价目表的形状。**用两个 float 起步，等于预约了一次数据迁移加一次历史账单重算。

### 3.2 表结构

价格**不放在 `models` 表上**。理由有三：价格变化频率远高于模型元数据；同一模型可以有多套价格（计费别名、协议价）；价格必须版本化而模型元数据不必。

```sql
-- 价目版本。一条记录 = 某个计费主体在某个时间段的完整价目
model_prices(
  id              bigserial,
  billing_model   text        NOT NULL,   -- 计费身份，对应 ModelNames.Billing（UNIFIED §5.3）
  scope_kind      text        NOT NULL,   -- 'default' | 'plan' | 'workspace'
  scope_id        text,                   -- plan 名 / workspace_id；default 时为 NULL
  currency        text        NOT NULL DEFAULT 'USD',
  rates           jsonb       NOT NULL,   -- 计价单元 → 费率，见下
  effective_from  timestamptz NOT NULL,
  effective_to    timestamptz,            -- NULL = 当前生效
  source          text        NOT NULL,   -- 'admin'（唯一写入口，见 §3.6）| 'upstream_diff_accepted'
  applied_by      text        NOT NULL,   -- 提交人。admin 是唯一写入口，此列不允许为空
  note            text
);
CREATE UNIQUE INDEX ON model_prices (billing_model, scope_kind, COALESCE(scope_id,''), effective_from);
CREATE INDEX ON model_prices (billing_model, effective_from DESC) WHERE effective_to IS NULL;
```

`rates` 的形状（**整数纳美元，见 §3.5**）：

```jsonc
{
  "input":            { "per_million_nano": 150000000 },        // $0.15 / 1M
  "output":           { "per_million_nano": 600000000 },        // $0.60 / 1M
  "cache_read":       { "per_million_nano": 15000000  },
  "cache_write":      { "per_million_nano": 187500000, "ttl": "5m" },
  "reasoning":        { "same_as": "output" },
  "input_long_context": { "threshold_tokens": 200000, "per_million_nano": 300000000 },
  "request_fixed":    { "per_request_nano": 0 }
}
```

- **`same_as` 是显式的**。"推理 token 与 output 同价"要写出来，而不是靠代码里的默认分支——否则某个厂商改成单列计价时，改动点找不到。
- **未声明的计价单元 = 不计费**，且必须在用量明细里显示为 0 而非空。"没配价"与"免费"在类型上要能区分：配置校验阶段就该拦住"模型有 cache_write 埋点但价目里没有 cache_write 费率"这种情况。

### 3.3 版本化：改一次价不能改掉历史账单

现状下改价，历史账单就会重算出不同结果。而 `upstream_diffs`（`ARCHITECTURE.md` §4.2）已经明确"改价这类操作必须留痕、先审后用"——**审阅记录留下了，但审完的价格落到哪张表、旧价格去哪了，没写。留痕留了一半。**

三条规则：

1. **价目只追加，不原地更新。** 改价 = 关闭旧版本（写 `effective_to`）+ 插入新版本。`upstream_diffs.applied_at` 与新版本的 `effective_from` 必须一致。
2. **`request_logs.price_version_id` 固化当时用的版本。** 没有这一列，对账时无法回答"上个月这笔为什么是这个数"。这是审计的地板，不是可选项。
3. **价目查询按 `received_at` 而非 `now()`。** 一次跨越改价时刻的长请求（流式可以跑几分钟），必须整体按请求**受理时**的价目结算，不能中途换价。取 `RequestContext.ReceivedAt`（`UNIFIED` §5.1）。

**改价预检**：应用价格变更前给出毛利影响预览（新价 × 近 7 日用量 vs 同期成本）。改价是 `upstream_diffs` 审阅的核心动作，审阅界面上光看价格数字变化不足以判断该不该应用。

### 3.4 价目解析顺序

四模型名里的 `Billing`（`UNIFIED` §5.3）已经为"同一模型多种价格"埋了伏笔（促销别名），但没有策略层。而 admin 侧已经定义了四种 plan（免费版 / 自助版 / 团队版 / 企业版）与 `monthlyBudget`。解析顺序定死为：

```
workspace 专属协议价  >  plan 价目  >  计费别名默认价  >  模型默认价
```

**每一层是整体覆盖，不是字段级合并。** 字段级合并（workspace 覆盖 input、plan 覆盖 output）看起来灵活，实际会产生"没人能说清这个模型现在多少钱"的价目，且排查成本极高。要覆盖就覆盖一整套费率。

解析结果必须可解释：结算时记录命中的 `price_version_id`，客服问"为什么这家收这个价"时能一眼追到那条记录。

**阶梯价（用量越大单价越低）本版不做。** 它会把计费从"每请求独立可计算"变成"依赖当期累计用量"，进而要求 `usage_rollups` 按计费周期而非 UTC 自然日聚合（`ARCHITECTURE.md` §4.3 现在是按 UTC 日）。若要做，走 §9-D 的路径：在 `workspaces` 上记当期累计，月末统一重算差额并以一条调整单入账，**不在热路径上做阶梯判断**。

### 3.5 金额精度：nano 计算，micro 落库

`ARCHITECTURE.md` §4.3 的论证是对的——"单次请求成本常在 $0.0001 量级，浮点累加百万次误差会进账单"，所以要用整数。但选的单位不够：

> `$0.15 / 1M tokens` = **1.5 × 10⁻⁷ USD/token**，比 1 微美元还小一个数量级。

若按"单价 × token 数"逐项相乘再取整到 micro，短请求（几十 token 的 embedding、1-token 的部署健康探针、单次 rerank）会被系统性截断到 0，**且偏差方向恒定**——这不是随机误差，是系统性少收。

规则：

| 环节 | 单位 | 说明 |
|---|---|---|
| 价目存储 | **nano USD**（1e-9）整数 | `per_million_nano` 存的是"每百万单位的纳美元数" |
| 计算过程 | **nano USD** 整数，全程不取整 | 每个计价单元算完直接累加 |
| 落库 | **nano USD**（`upstream_cost_nano` / `charged_amount_nano`） | 请求级不再降精度 |
| 账单/对外展示 | micro USD 或 2 位小数 | **只在这一步取整，且只取整一次** |

两个实现约束：

- **溢出边界要算清。** int64 上限 ≈ 9.22e18 nano ≈ **$9.22e9**，余额与账单绝无问题；**危险在中间乘积**：`tokens × per_million_nano` 在 1e7 token × $1000/1M 的极端组合下会溢出。实现必须先除后乘（`(n/1e6)*rate + (n%1e6)*rate/1e6`）或走 `big.Int`，并加单元测试锁住这个边界。
- **舍入规则写死**：对**请求总额**做一次 round-half-up，不对每个计价单元分别舍入。分别舍入会让"分维度求和"与"总额"对不上，这类差异在对账时极其难查。

### 3.6 写入路径：admin 是价目的唯一入口

web 端展示给用户的模型 ID、描述、厂商、价格等全部由 admin 录入并提交。这一条对计价设计有五个直接后果，且**其中三个是本文其余章节的前提**。

**① 上游同步不写价，只产出建议。**

`upstream_diffs`（`ARCHITECTURE.md` §4.2）与 admin 的 `UpstreamDiff` / `DiffEntry` 类型已经是这个形态——抓取上游变更 → 进入审阅队列 → 人工决定是否应用。据此收敛：

```
上游 /models 同步  ──→  upstream_diffs（建议，无计费效力）
                              │ admin 审阅并接受
                              ▼
                         model_prices 新版本（source='upstream_diff_accepted'）
```

**没有任何路径能绕过 admin 直接改价。** 这也让 `UNIFIED-PROVIDER-INTERFACE.md` §11-F 的"是否自建签名目录 feed"这个未决项失去了紧迫性：既然价格无论如何都要人工过一遍，feed 的价值退化为"少填几个字段"，首版按该文的建议走静态数据 + 上游同步即可。

**② 展示价与计费价必须是同一条记录。**

这是本节最要紧的一条。web 上显示 `$0.15 / 1M`，用户按 `$0.15` 付费——两个数字若来自不同的表或不同的解析路径，迟早会不一致，而这类不一致**用户一定会先于我们发现**。

```
catalog 展示  ─┐
               ├─→ 同一条 model_prices（scope_kind='default'，当前生效版本）
计费结算      ─┘
```

`/models` 与 `/models/{provider}/{model}` 的价格字段**必须由与结算相同的价目解析函数产出**（§3.4），而不是另写一个 catalog 专用查询。这一条要在实现时用同一个 sqlc 查询 + 同一个解析函数强制，不能靠"注意保持一致"。

推论：**web 展示的是 `default` 档价格**（未登录用户也能看）。若某个 workspace 有协议价（§3.4），它在 console 里看到的价格与公开目录不同——这是对的，但要在 UI 上标明"您的协议价"，否则会被当成 bug 报上来。

**③ 模型状态机与价目生效时间是正交的两件事。**

admin 的 `CatalogModel.status` 已有四态：`draft | listed | canary | delisted`。它管的是**可见性与可调用性**，价目版本管的是**钱**。两者不能合并：

| 状态 | web 可见 | 可调用 | 价目要求 |
|---|---|---|---|
| `draft` | 否 | 否 | 可以没有价目 |
| `canary` | 否（或灰度可见） | 是，限灰度流量 | **必须有价目**——有调用就有计费 |
| `listed` | 是 | 是 | **必须有价目** |
| `delisted` | 否 | 否（存量 key 视策略） | **价目不可删**，历史账单要能重算 |

两条硬规则：

- **`draft → listed/canary` 的发布动作必须校验价目完整性**（见 ④）。一个能被调用却没有价目的模型，请求进来时只有两种结局：按 0 计费（白送）或直接 500。两种都不可接受，而**发布时拦住的成本远低于运行时处理**。
- **`delisted` 只关闭可见性与新调用，绝不删除价目版本。** 这是 §3.3 版本化的直接推论。

**④ 校验必须前移到 admin 提交时。**

既然价目由人手工填，那么"填错"是常态而非异常。以下检查全部放在**提交/发布的写路径**上，运行时只做兜底：

| 校验 | 拦截理由 |
|---|---|
| 计价单元与埋点匹配 | 模型 `capabilities` 声明支持缓存，价目却没有 `cache_read` 费率 → `request_logs` 记了 token 但算不出钱（§3.2） |
| 输出价 ≥ 输入价（**告警而非阻断**） | 绝大多数模型如此，反过来通常是把两个数填反了。少数模型确实相反，所以只提示 |
| 数量级异常 | 与上游标价偏离一个数量级以上 → 极可能是单位填错（把"每千 token"当成"每百万 token"填了，差 1000 倍） |
| 毛利预览 | 新价 × 近 7 日用量 vs 同期成本（§3.3）。**毛利为负必须二次确认**，不能静默保存 |
| 自建模型必须有售价 | 见 ⑤ |

第三条尤其值得做。手填价格最常见的错误不是"填了个略高/略低的数"，而是**单位错位**——这类错误一旦上线，几分钟内就是几个数量级的资损或白送，且用户不会来报"你们收得太便宜了"。

**⑤ 自建模型现在没有售价，这是个产品缺口。**

`frontend/admin/src/mock/catalog.ts` 里所有 `hosting: 'self'` 的模型，`inputPrice` / `outputPrice` 都是 `null`：

```ts
{ id: 'selfhosted/qwen3-max', vendor: '自建', hosting: 'self', status: 'canary',
  inputPrice: null, outputPrice: null, ... }
```

而其中一个是 `canary`——**已经在接真实流量了**。按 §4.2，自建模型的售价与云端模型没有任何区别：都锚定 `Billing` 名，都是 admin 定的一个数；不同的只是成本侧（云端是上游账单，自建是摊销值，§4.4）。

> **已定（2026-09-08）**：**自建模型对标同档云端模型定固定价**，与云端模型走完全相同的价目机制。
> 售价固定、成本浮动，两者的差就是毛利（§4.2）。**"按成本价浮动"已明确排除**——成本是利用率的函数、
> 当期结束才准（`SELF-HOSTED` §4），拿它当售价等于价格每天变。

落地上还差一步：**对标基准与折扣比例需要给出具体数字**，这是商业决策不是技术决策。形态是

```
自建售价 = 对标云端模型的价格 × 折扣系数
```

对标关系建议**落到 `model_prices.note` 里写明**（如"对标 xx-72b，取 75%"），而不是只存一个结果数字。上游那个对标模型涨价时，我们才知道该不该跟——否则半年后没人记得这个 `$0.42` 是怎么来的。

在数字给出之前，`selfhosted/qwen3-max` 应当**从 `canary` 退回 `draft`**：它现在在接真实流量但无价可计，每一个请求都是白送且无法事后追补（§3.3 的价目按 `received_at` 解析，事后新增的价目版本不会覆盖历史请求）。

**⑥ 改价是破坏性操作，走审计与二次确认。**

`audit_logs.destructive`（`ARCHITECTURE.md` §4.4）是布尔标记，admin 审计页支持"仅看破坏性操作"一键筛选。**改价必须标 `destructive = true`**——它不删除任何数据，但它直接改变收入，是审计时最需要一眼看到的一类操作。

建议再加一层：**已有真实调用量的模型（近 7 日调用 > 0）改价时，要求二次确认并强制填写 `note`。** 事后追查"这次改价是谁、为什么"时，`note` 的价值远高于一条只有时间戳的审计记录。

---

## 4. 计费口径：按谁的数算、什么算钱

### 4.1 usage 的三档可信度

`UNIFIED` §4.4 的 Quirks 第一条就是 `NoStreamUsage bool // 不支持 stream_options.include_usage`——**方案自己已经承认"上游不给 usage"这个场景存在**，但没有任何一处说这种情况按什么收费。

定义三档，且**必须落到 `request_logs.usage_source` 字段上**：

| 来源 | 取值 | 处置 |
|---|---|---|
| 上游返回的 usage | `upstream` | **以上游为准，即使我们认为它算错了。**否则与上游的对账永远对不平 |
| 自建 vLLM 返回的 usage | `self_hosted` | 可信，等同 `upstream` |
| 本地 tokenizer 估算 | `estimated` | 上游不给时的兜底 |

`estimated` 的三条纪律：

1. **不得静默按精确值计费。**用量明细页要能区分，且导出的账单附注里要写明估算条目数。
2. **估算偏保守**（宁可少收）。系统性多收估算值是不可辩解的；少收是我们自己承担适配缺口的代价。
3. **`estimated` 占比是个运维指标。**某个渠道的估算占比上升，意味着它的 usage 返回退化了，应当告警——这通常是上游行为漂移的早期信号（`UNIFIED` §2.3 提到的问题）。

### 4.2 售价与路由供给必须解耦

`SELF-HOSTED` §2 给了 `costFactor` 三档（自有机房 / 云上实例 / 云端 API），**成本分档了，但没有一句话说明用户看到的价格是否跟着变**。三层供给让这从一个理论问题变成了真实风险。

定死：

> **售价锚定 `ModelNames.Billing`，与实际路由到哪一层供给完全无关。**

价差就是毛利：路由到自有机房赚多一点，溢出到云端 API 赚少甚至亏损。**这是运营问题，不是计价问题。** 否则用户会发现同一个 prompt 两次调用价格不同——在 API 产品里这是致命的信任问题，且会诱导用户去猜测和规避我们的路由。

`UNIFIED` §5.3 引用 new-api 那句 *"virtual pricing aliases never participate in channel selection"* 说的是**计费不影响路由**；这里补的是**反方向**：路由不影响计费。两条都要成立，计费与路由才算真正正交。

### 4.3 重试与失败的计费

回退链换渠道重试时，**前面失败的渠道可能已经产生了真实上游费用**——prefill 已经发生、429 之前的部分消耗都是要付钱的。文档里唯一相关的一句是 `UNIFIED` §7「计费按实际产出 token 结算」，那说的是向用户收多少，没说我们付出去的算在哪。

规则表（与 `UNIFIED` §5.5 的回退边界条件表一一对应）：

| 情况 | `upstream_cost` | `charged_amount` |
|---|---|---|
| 换渠道重试，前序尝试的消耗 | **计入**（真实付出了） | **不计**（用户不该为我们的路由失败买单） |
| 连接失败 / DNS / 未发出 | 0 | 0 |
| 上游 4xx 参数错误（不重试） | 通常 0，按上游账单为准 | **不计**。请求没产出，收费不合理 |
| 流式首字节前失败并成功重试 | 计入各次尝试 | 只按最终成功的那次 |
| **`partial_failure`**（首 chunk 后中断） | 计入实际消耗 | **按实际产出 token 计**（`UNIFIED` §7） |
| 客户端主动断开 | 计入（上游已在生成，且我们已取消） | 按取消前实际产出计 |
| **平台自身发起的探测**（自动健康探针、admin 渠道测试） | **计入**，`workspace_id = NULL`、`origin='admin_probe'` / `'health_probe'` | **0**。不属于任何客户 |

最后一行容易漏。探测请求量不大但持续存在，不记账它就会变成上游账单里对不上的一块，而 §11-F 的差异率监控会因此持续报警、排查方向还完全是错的。口径细节见 `UNIFIED-PROVIDER-INTERFACE.md` §5.6。

两条推论：

- **`request_logs` 是 attempt 级还是 request 级？** 定为**每次 attempt 一行**，用 `request_id` 关联，`is_final` 标记最终成功那次。只记最终一次会丢掉重试成本，而重试成本恰恰是渠道健康度劣化最直接的财务表现。
- **重试成本占比要能查。** 这条数据不单列，渠道劣化会以"莫名亏损"的形式出现，而不是以"重试率升高"的形式出现——排查方向会完全跑偏。

### 4.4 自建供给：摊销值只进成本，永不进账单

`SELF-HOSTED` §4 已经论证清楚自建单位成本是**利用率的函数而非常数**（20% 利用率下可能比云端 API 还贵）。计费侧的落地约束是三条：

1. 自建请求的 `charged_amount_nano` 在请求发生时**即刻确定**（走 §3 的价目，与云端请求毫无区别）。
2. 自建请求的 `upstream_cost_nano` 在请求发生时先填**基于上期利用率的估算值**，并置 `cost_estimated = true`；当期结束后由后台任务按 §3 的公式回填真值并清除该标记。
3. **回填任务禁止触碰 `charged_amount_nano`。** 这条应当在代码层强制——回填 SQL 走独立的 sqlc 查询，只 UPDATE 成本列；CI 里加一条断言，任何 UPDATE 到 `charged_amount_nano` 的语句只能出现在结算路径。

第 3 条看起来啰嗦，但 §2 已经说明这是本方案识别出的最危险的一处冲突。约束不落到代码上就等于没有。

---

## 5. 扣费流程：预扣 — 结算 — 释放

`SERVICE` §4 有 `QuotaService` 接口签名，`UNIFIED` §5.1 有 `BillingSession` 接口签名，**两处都只有方法名，没有语义**。这一节补语义。

### 5.1 三阶段与它们的真实职责

```
① PreConsume  受理请求后、发往上游前   预扣估算额，拿到 reservation
② Settle      拿到 usage 后             按实际用量结算，多退少补
③ Release     异常路径                  归还预扣，写明原因
```

同步 chat 与异步任务共用这套流程，异步任务多一个 `AdjustOnSubmit`（`UNIFIED` §6，视频实际时长与请求参数不符时调整），语义以那里为准。

### 5.2 预扣估算公式

流式请求在发出前根本不知道 output token 数，这是预扣估算存在的全部理由。

```
estimate = input_tokens        × input_rate
         + cache_write_tokens  × cache_write_rate
         + est_max_output      × output_rate
         + request_fixed

est_max_output = min(
    请求里的 max_tokens（若有）,
    模型的 max_output_tokens,
    workspace.max_estimate_tokens    -- 默认 8192，可按 plan 调
)
```

三个判断：

- **`max_tokens` 缺省时不能取模型上限。** 模型上限可能是 32k 甚至更高，全额预扣会把余额充裕的用户也误伤成"余额不足"。取一个可配的保守默认（8192）并允许 workspace 覆盖。
- **估算额超过可用余额 → 直接拒绝，`402` + 明确错误码。** 不做"先跑跑看"，那正是超额的来源。
- **预扣的是估算额，结算的是实际额，差额在 `Settle` 时归还。** 因此预扣偏高只影响"并发请求能开多少个"，不影响最终收费——这个权衡要在错误信息里向用户解释清楚，否则会收到"我明明还有余额"的工单。

### 5.3 Reservation 的生命周期与悬挂回收

**这是本方案里唯一真实的坏账窗口。** `ARCHITECTURE.md` §6.3 那段"不会因此产生坏账"的论证只覆盖了日志批量写的丢失窗口，**没有覆盖预扣的悬挂窗口**：Redis 里预扣了，进程被 kill，这笔钱谁来退？

```sql
reservations(
  id            bigserial,
  request_id    text        NOT NULL UNIQUE,   -- 幂等键
  workspace_id  bigint      NOT NULL,
  amount_nano   bigint      NOT NULL,
  status        text        NOT NULL,          -- held | settled | released | expired
  expires_at    timestamptz NOT NULL,
  created_at    timestamptz NOT NULL,
  settled_at    timestamptz
);
CREATE INDEX ON reservations (status, expires_at) WHERE status = 'held';
```

**真相源在 Postgres，Redis 只做原子计数裁决**——这与 `ARCHITECTURE.md` §2.5 定的边界一致，不能因为"Redis 上有 TTL 就够了"而破例。Redis key 的 TTL 是最后一道防线，正常路径必须显式释放。

TTL 取值：

```
TTL = firstByteTimeout + stallTimeout × 预期 chunk 数上限 + 安全余量
    → 同步 chat 取 15 分钟硬上限
    → 异步任务按任务预计时长，可到小时级（走 UNIFIED §6 的三阶段）
```

**悬挂回收任务**（river 定时，每分钟）：扫 `status='held' AND expires_at < now()` → 归还 Redis 计数 → 标记 `expired`。

关键的边界情况：**回收之后，迟到的 `Settle` 到达了怎么办？**（进程重启后恢复、或上游响应极慢）——凭 `request_id` 幂等地走**补记路径**：直接扣减实际金额，**允许余额为负到 `credit_limit`**，并记一条 `settle_after_expiry` 审计。理由是这笔消耗真实发生了，拒绝入账等于我们自己吃掉；而补记造成的负余额会在下次 `PreConsume` 时挡住新请求，损失有界。

### 5.4 幂等

`Settle` **必须**以 `request_id` 为幂等键。回退链上一次请求可能触发多次结算尝试（重试、进程恢复、对账补记），不幂等就会重复扣款。

实现：`reservations.request_id` 上的唯一约束 + `UPDATE ... WHERE status='held'` 的条件更新，受影响行数为 0 即视为已结算，直接返回成功。**不要用"先查后写"**——那在并发下是错的。

### 5.5 Redis 与 Postgres 对账

`ARCHITECTURE.md` §6.3 说"Postgres 为真相源并周期性对账"，但**周期多久、发现差异以谁为准、差多少要告警，三个参数一个都没定**。不定这三个，对账等于没做。

| 参数 | 取值 | 说明 |
|---|---|---|
| 周期 | **5 分钟** | 比日志 flush 间隔（1–5s）粗得多即可，这里对的是余额不是流水 |
| 等式 | `Redis 可用额 == PG 余额 - Σ(held reservations)` | 两边都从权威数据重算，不信任任何缓存中间值 |
| 以谁为准 | **Postgres**，按上式重建 Redis | 无条件。Redis 崩溃后可完全重建，这是 §2.5 那条边界的兑现 |
| 告警阈值 | 任何非零漂移都记录；**> $0.01 或连续 3 个周期非零** 则告警 | 单次微小漂移可能是对账瞬间的在途请求，持续漂移一定是 bug |

对账窗口本身要处理在途请求：取一个 Postgres 快照时间点，只统计 `created_at <= 快照点` 的 reservation，避免把正常的并发写当成漂移。

### 5.6 异步任务

三阶段计费的语义以 `UNIFIED` §6 为准，本文不重复。只补两条与本文其他部分的衔接：

- `EstimateCost` 走 §5.2 的公式，但按 `video_per_second` / `image_per_unit` 这类单元估算。
- `SettleOnComplete` 走 §5.4 的幂等规则，且**必须处理"上游任务永不返回终态"**——超过 TTL 走 §5.3 的回收路径，任务标记 `abandoned` 并全额退还。上游后续再返回结果的，走补记。

---

## 6. 余额、赠送额度与信用额度

### 6.1 预付定案

> **已定（2026-09-08）**：**预付**。余额强一致扣减；企业月结用 `credit_limit_nano` 表达，
> **首版建列但保持 0、不做产品化**。该列无论如何都必须存在——§5.3 的补记路径要求余额可短暂为负。
> 以下是当初的论证，保留备查。

`ARCHITECTURE.md` §10-C「计费模型（预付/后付）」当时仍挂着未决，但三处已经把它锁死：

- §6.3 已按**强一致余额扣减**设计（Redis 原子裁决）
- `SERVICE` §1 把"计费强一致"作为**否决服务拆分**的理由
- 前端 console 已显示"余额 $42.80 + 自动充值"

**建议直接结案为预付**，并把企业月结作为例外能力实现，而不是引入第二套计费模型：

```
workspaces.credit_limit_nano   bigint  DEFAULT 0   -- 允许余额为负到这个额度
```

`credit_limit = 0` 即纯预付（绝大多数用户）；企业客户按合同给额度，月末按实际用量出账单。**用信用额度表达后付，比维护两套扣费路径便宜一个数量级**，且 §5.3 的补记路径本来就需要允许负余额，两处诉求合流。

### 6.2 余额分层与扣减顺序

`balance_usd` 单一字段无法表达赠送额度（有效期、不可提现、不同的会计处理）。拆成账目流水：

```sql
credits(
  id, workspace_id,
  kind         text,        -- 'grant'（赠送/免费额度） | 'topup'（充值） | 'adjustment'（人工调整）
  amount_nano  bigint,      -- 正数入账
  remaining_nano bigint,    -- 剩余
  expires_at   timestamptz, -- grant 才有；topup 通常为 NULL
  created_at
);
```

**扣减顺序：先过期的先扣 → grant 先于 topup → 同类按 FIFO。** 这个顺序对用户最有利（赠送额度过期就浪费了），也最符合直觉。`workspaces.balance_nano` 保留为物化的汇总值，由触发器或结算路径维护，用于热路径快速判断，**但真相在 `credits`**。

赠送额度过期由 river 定时任务扫描并写一条负向 `adjustment`，不直接改 `remaining_nano`——账目只追加不修改，这是能对得平账的前提。

### 6.3 配额与余额是两回事

`quotas` 表（`ARCHITECTURE.md` §4.3）管的是**用量上限**（tokens/requests/预算），余额管的是**钱**。两者都要在 `PreConsume` 时检查，但语义不同：

| | 余额不足 | 配额耗尽 |
|---|---|---|
| HTTP | `402` | `429`（附 `X-RateLimit-*`） |
| 恢复方式 | 充值 | 等待周期重置 / 提升配额 |
| 谁设的 | 用户自己 | 工作空间管理员 / 平台 |

`workspaces.monthly_budget_usd` 是**软预算**（到达时告警、可选拦截），与 `credit_limit` 是硬边界不同。admin 的 `Workspace.monthlyBudget / monthlySpend` 对应的正是这一对。

---

## 7. 账单与发票

`ARCHITECTURE.md` §4.3 的 `invoices/topups ...` 是字面的省略号。admin 侧的类型已经定好了（`frontend/admin/src/api/contracts.ts` 的 `Invoice` / `Topup` / `BillingPayload`），照着落表即可：

```sql
invoices(id, workspace_id, period_start, period_end, amount_nano,
         status,            -- draft | pending | paid | overdue | void
         issued_at, due_at, paid_at, line_items jsonb, pdf_url)
topups(id, workspace_id, amount_nano, method, provider_txn_id,
       status,              -- pending | success | failed | refunded
       created_at, completed_at)
```

四条约束：

- **账单出账后不可变。** 需要修正走**红冲 + 重开**（新开一张负向 invoice），不是 UPDATE。这是会计的基本要求，也是 §2 那条"售价一经结算不可变"的延伸。
- **`line_items` 按 `Billing` 模型名 + 计价单元分组**，并附 `price_version_id`。用户问"为什么这个月贵了"，明细要能自证。
- **`topups.provider_txn_id` 唯一**。支付回调必然会重复投递，唯一约束是最省事也最可靠的幂等手段。
- **对账口径**：`Σ line_items == invoice.amount == Σ 该周期 request_logs.charged_amount`。这条等式要有定时校验任务，不等到用户投诉才发现对不上。

**币种**：契约现在把 `currency` 写死成 `const: USD`（`openapi.yaml` `ModelSummary.pricing`）。结算币种保持 USD 没问题，但**建议契约改成 `enum` 而非 `const`**——若将来有人民币结算/开票需求，`const` 是个需要改契约的硬编码，而 `enum` 只需加值。这是一行成本的前瞻。

---

## 8. 成本归集与毛利

有了 §2 的两本账，这一层是纯查询：

```
毛利     = Σ charged_amount_nano - Σ upstream_cost_nano
毛利率   = 毛利 / Σ charged_amount_nano
```

必须能按这几个维度切：模型（`Billing` 名）/ 供给层（自有机房 / 云上实例 / 云端 API）/ 渠道 / workspace / plan。

三条运营护栏：

1. **模型级毛利率告警。** 某个模型的毛利率转负，通常意味着上游涨价了而我们的价目没跟——`upstream_diffs` 的价格差异如果长期没人审，这里就是它的后果显现处。
2. **改价预检**（§3.3）：应用价格变更前给出毛利影响预览。
3. **自建的盈亏平衡利用率**（`SELF-HOSTED` §4）在 admin 分析页展示时，必须与云端的"供应商计价"分开标注，并附当期利用率。**低于盈亏平衡线时，"自建更便宜"是错的**——这是该文已经论证过的，本文只是把它接到毛利视图上。

`usage_rollups`（`ARCHITECTURE.md` §4.3）要相应扩列：`charged_amount_nano` / `upstream_cost_nano` / `estimated_usage_count`，按 UTC 日 + workspace + billing_model + supply_tier 聚合。

---

## 9. 数据模型汇总

本文新增/修改的表与列，改动请改这一处：

```sql
-- 新增
model_prices(...)              -- §3.2，价目版本
reservations(...)              -- §5.3，预扣
credits(...)                   -- §6.2，余额分层
invoices(...) / topups(...)    -- §7，展开 ARCHITECTURE §4.3 的省略号

-- 修改 request_logs（ARCHITECTURE §4.3）
- cost_usd_micro                          → 删除
+ upstream_cost_nano   bigint             -- 成本，可回填
+ charged_amount_nano  bigint             -- 售价，不可变
+ price_version_id     bigint             -- 引用 model_prices
+ usage_source         text               -- upstream | self_hosted | estimated
+ cost_estimated       boolean            -- 自建摊销待回填
+ is_final             boolean            -- attempt 级记录里最终成功的那次
+ billing_model        text               -- ModelNames.Billing，与 model_id 分开

-- 修改 workspaces（ARCHITECTURE §4.1）
- balance_usd                             → balance_nano（单位与精度对齐 §3.5）
+ credit_limit_nano    bigint DEFAULT 0   -- §6.1
+ max_estimate_tokens  int                -- §5.2 预扣估算上限

-- 修改 usage_rollups
+ charged_amount_nano / upstream_cost_nano / supply_tier / estimated_usage_count
```

**分区**：`request_logs` 改为 attempt 级后行数上升（重试率通常个位数百分比，影响有限），`ARCHITECTURE.md` §4.3 的按日分区策略不变。`reservations` 不分区——它是短生命周期表，`settled`/`released` 的记录保留 90 天后归档删除即可。

---

## 10. 实施顺序

对应 `ARCHITECTURE.md` §9 的 **B5（计量计费）**，可细分为：

| 步骤 | 内容 | 判据 |
|---|---|---|
| **M1** | `model_prices` 表 + 价目解析（§3.2/3.4）+ nano 精度与溢出测试（§3.5）+ **admin 写入口与提交校验**（§3.6） | 任取一个模型能解析出完整费率；短请求（20 token）金额不为 0；**catalog 展示价与结算价走同一个解析函数**；无价目的模型无法发布为 listed/canary |
| **M2** | `request_logs` 三字段拆分（§2）+ `usage_source`（§4.1） | 成本与售价可分别查询；估算条目可筛选 |
| **M3** | `reservations` + 预扣/结算/释放 + 幂等（§5.1–5.4） | 并发压测下不超额；kill -9 后悬挂预扣能被回收 |
| **M4** | Redis ↔ PG 对账任务（§5.5） | 注入人工漂移能被检出并告警 |
| **M5** | `credits` 分层与扣减顺序（§6.2）+ `credit_limit`（§6.1） | 赠送额度先扣、过期作废可验证 |
| **M6** | 账单与发票（§7） | admin BillingPage 接真实数据；三方对账等式成立 |
| **M7** | 毛利视图 + 改价预检（§8）+ 自建摊销回填（§4.4） | 分析页显示毛利与摊销成本；回填不改账单 |

**M1–M3 必须在 B3（数据平面 MVP）之后立刻做，不能推到 B5 末尾。** 理由与 `ARCHITECTURE.md` §9 建议"B3 早做"是同一个：`request_logs` 的字段形状一旦有真实数据写入，再改就要迁移 + 重算。**M2 尤其是**——它只是三个字段的取舍，现在改是零成本，等有了一个月的生产数据就是一次数据迁移加一次账单口径解释。

**M3 是风险最高的一步。** 预扣的悬挂回收（§5.3）与幂等（§5.4）必须有针对性的故障注入测试：进程 kill、Redis 重启、上游超时后迟到响应，三种都要跑。这类 bug 在生产暴露时的形式是"用户余额对不上"，代价远高于测试成本。

---

## 11. 需要确认的问题

> ~~**A. 预付定案。**~~ **已定（2026-09-08）**：预付，余额强一致；`credit_limit_nano` 建列保持 0，首版不产品化。B5 解除阻塞。
>
> **派生的未决项**：前端 console 已画"自动充值"，需要支付通道。首版若不接支付，`topups` 只能由 admin 手工入账，前端要处理该入口的状态。不阻塞 M1–M5。

> ~~**B. 免费额度政策。**~~ **已定（2026-09-08）**：**按月重置的 `grant`**，用完即停，月初重新发放。
> 落到 §6.2 的模型上是一条 `credits(kind='grant', expires_at=次月初)` 记录 + 一个 river 定时任务
> （过期作废写负向 `adjustment`，同时发放新一期）。**扣减顺序天然正确**：grant 先于 topup、先过期的先扣。
>
> **仍需给出的数字**：每月额度多少。建议按"够跑通一个小项目、不够拿来当生产用量"来定。

> ~~**C. 缓存计价是否首版就做。**~~ **已定（2026-09-08）**：**首版就做 `cache_read`，跟随上游给折扣**（按输入价的约 10%，具体随各厂商口径）。
> 理由是成本侧本来就只付 10%，跟随折扣毛利不变；而"命中缓存真的便宜"是 Agent 类客户选平台的真实理由——
> 那类客户的系统提示词会被反复复用，缓存折扣直接决定他们的账单。
>
> `cache_write` **首版按上游有无决定**：上游区分写入价的（如 Anthropic 的 5m/1h 两档）就照配，
> 不区分的则不设该费率。注意 §3.2 那条——**不设 ≠ 留空**，价目校验会拦住"有 cache_write 埋点但无费率"的组合。

> ~~**D. 阶梯价是否需要。**~~ **已定（2026-09-08）**：**首版不做**。理由是它会把计费从"每请求独立可计算"
> 变成"依赖当期累计用量"，进而要求 `usage_rollups` 改按计费周期聚合。将来若商业上需要，走
> §3.4 给的路径：月末重算差额 + 一条调整单入账，**永不放进热路径**。表结构无需为此预留字段。

**E. 发票与税务。** §7 只做了账单，没做税。若面向企业客户开票（尤其是增值税），需要额外的税率模型与开票主体信息，且币种（§7 末）要重新评估。这是产品/合规决策，不阻塞 M1–M5。**支付通道已定为首版不接**（见下），税务因此可以一并推迟——但两者要一起决策，接国内支付通常意味着同时需要开票。

> ~~**F. 上游账单对账。**~~ **已定（2026-09-08）**：**只做差异率监控**，不做逐条对账。
> 我们的 `upstream_cost` 与上游月末账单必然有差异（计量口径、汇率、优惠、探测请求），
> 逐条对账的成本远高于收益。做法：按渠道按月比对总额，差异率超阈值（建议 3%）告警并人工介入。
>
> **前置条件**：平台自身发起的探测必须记账（§4.3 最后一行），否则这个监控会持续报假警。

> ~~**G. 自建模型对用户的售价。**~~ **已定（2026-09-08）**：对标同档云端模型定固定价，机制与云端模型完全相同（§3.6-⑤）。M1 解除阻塞。
>
> **折扣系数已定（2026-09-08）：70%**，即 `自建售价 = 对标同档云端模型价格 × 0.7`。
> 按 `SELF-HOSTED-GPU-OPERATIONS.md` §4 的数量级示意，利用率 50% 以上仍有毛利，**20% 利用率下会亏**——
> 因此 G5 的摊销成本监控与盈亏平衡利用率展示不能推迟太久，它是这个折扣是否站得住的唯一验证手段。
>
> **剩下的是逐个模型的对标关系**（哪个自建模型对标哪个云端模型），录入时写进 `model_prices.note`。
> 在对标关系给出前，`selfhosted/qwen3-max` 应从 `canary` 退回 `draft`——它现在在接真实流量但无价可计，
> 且**事后无法追补**（价目按 `received_at` 解析，新版本不覆盖历史请求）。

**H. 支付通道 —— 已定（2026-09-08）：首版不接，`topups` 由 admin 手工入账。** 对应银行转账/对公汇款后人工充值。

三条实现后果：

- **前端 console 的"自动充值"入口必须隐藏或置灰**，不能留一个点了没反应的按钮。
- `topups.provider_txn_id` 仍然保留（§7），手工入账时填银行流水号——**幂等约束照样有用**，它防的是同一笔转账被录入两次。
- 手工入账是**破坏性操作**：直接增加客户余额。必须走 `audit_logs` 且标 `destructive = true`，并强制填写凭证号与 `note`（与 §3.6-⑥ 的改价同一处置）。

接支付通道时（Stripe 或国内支付）要一并重新评估 §11-E 的税务与 §7 末的币种。

---

## 附一：契约改动提案（待单独评审）

`frontend/contracts/openapi.yaml` 是前后端共用的契约，本节只提出改动与理由，**不随本文一起生效**。改动全部集中在定价相关的三处：`ModelSummary.pricing`、`ModelEndpoint` 的价格字段、`/models` 的价格过滤与排序。

**现在改的成本是零。** 契约的实现方还不存在（B2 未开始，web 仍在用 `data.ts`），等有了后端实现和真实数据，同样的改动就是一次前后端联动加一次缓存失效。

### 附 1.1 三个已经暴露的问题

**① 前端在自己发明价格。** `frontend/web/src/App.tsx:122` 的 Pricing 区块：

```tsx
<Metric label="CACHE READ" value={`$${(model.input*.1).toFixed(3)}`} note="/ 1M tokens" />
```

缓存读取价是**输入价 × 0.1 硬编码猜出来的**，因为契约里没有这个字段。这个数字会被用户拿去做选型决策，而各厂商的缓存折扣实际从 10% 到 50% 不等，自建模型更是没有缓存计价一说。

这正好违反 `ARCHITECTURE.md` §6.4 自己立的规矩——那里为了不猜 Agent 框架，宁可让 admin 页面保持空状态，理由是"假数据会被拿去做决策"。**同一条原则在价格上更适用**：猜错框架分布是分析失真，猜错价格是用户按错误信息付钱。

**② `ModelEndpoint` 的端点级价格与 §4.2 直接冲突。** 契约在端点上挂了 `input_per_million` / `output_per_million`（`openapi.yaml:588-589`），字面读法是"不同端点价格不同、用户按选中的端点付费"。这是 OpenRouter 的模型，**不是本方案的模型**——§4.2 已经定死售价锚定 `Billing` 名、不随路由波动。

两条路，必须选一条并写进契约描述：

- **推荐**：端点价格重命名为 `upstream_list_price_input_per_million` / `..._output_...`，`description` 明确写「上游标价，仅供透明度展示，**不是计费依据**」。保留它有真实价值——用户能看到我们把请求路由到了更便宜的供给，这是信任加分项。
- 或者：直接删掉。若产品上不打算公开供给侧成本，留着只会引发"我到底按哪个价付钱"的疑问。

**③ `sort=price` 与 `max_input_price` 的语义没定义。** 多维价格下"按价格排序"是有歧义的（按输入价？输出价？某个加权？）。且 `input_per_million: null` 的含义是**未知**，前端 `App.tsx:84` 的"免费"筛选却用 `m.input === 0` 判断——**null 一旦被当成 0，未定价的模型会被展示为免费**。契约必须写死：排序按 `input_per_million`，null 恒排在最后，且 null ≠ 0。

### 附 1.2 提议的 `ModelSummary.pricing`

设计原则两条：

1. **契约暴露解析后的有效价格，不暴露内部表达。** §3.2 的 `rates` 里有 `{"reasoning": {"same_as": "output"}}` 这种写法，那是价目表的存储形态；契约里 `reasoning_per_million` 必须是已经解析好的数字。否则每个客户端都要实现一遍解析规则，迟早出现两种算法。
2. **可选字段的缺省 = 该模型不适用该计价单元，不是 0。** 与现有 `[number, 'null']` 的 null=未知 并存，三态要在 description 里写清。

```yaml
    ModelPricing:
      type: object
      required: [currency, input_per_million, output_per_million]
      description: >
        有效价格，已解析别名与折扣。字段缺省 = 该模型无此计价单元；
        值为 null = 价格未知（不等于免费，UI 必须显示为「—」而非 $0）。
      properties:
        currency: { type: string, enum: [USD], default: USD }   # 原为 const，改 enum 留出币种扩展
        input_per_million:  { type: [number, 'null'], minimum: 0 }
        output_per_million: { type: [number, 'null'], minimum: 0 }
        cache_read_per_million: { type: number, minimum: 0 }
        cache_write:
          type: array
          description: 缓存写入价。Anthropic 的 5m / 1h 两档不同价，故为数组
          items:
            type: object
            required: [ttl, per_million]
            properties:
              ttl:         { type: string, enum: ['5m', '1h'] }
              per_million: { type: number, minimum: 0 }
        reasoning_per_million:
          type: number
          description: 推理 token 单价。与 output 同价时也必须显式给出数值，不留空
        request_fixed:
          type: number
          description: 每请求固定费，多数模型为 0 时应省略而非填 0
        tiers:
          type: array
          description: 长上下文等分档价，按 above_tokens 升序。空或缺省 = 单一价格
          items:
            type: object
            required: [above_tokens, input_per_million, output_per_million]
            properties:
              above_tokens:       { type: integer, minimum: 1 }
              input_per_million:  { type: number, minimum: 0 }
              output_per_million: { type: number, minimum: 0 }
        effective_from:
          type: string
          format: date-time
          description: 该价目的生效时间（对应 model_prices.effective_from），供 UI 标注价格新鲜度
```

`effective_from` 值得单独说：模型价格是会变的，而 catalog 的缓存策略是 `max-age=60`。用户看到一个价格却按另一个价格被收费，是最难解释的一类工单。把生效时间放进契约，UI 就能在改价窗口内显示"价格更新于 X"，成本几乎为零。

### 附 1.3 媒体模型不走这个 schema

图像/音频/视频的计价单元是按张、按秒（§3.1），与 token 价没有公共形状。硬塞进 `ModelPricing` 会让大半字段恒为 null——这正是 `UNIFIED-PROVIDER-INTERFACE.md` §5.2 把 `models` 与 `media_models` 分表的同一个理由。

**建议契约侧同样分开**：媒体模型用独立的 `MediaModelSummary` + `MediaPricing`（`unit: per_image | per_second`），而不是给 `ModelPricing` 加一堆可选字段。这一项可以推到媒体能力上线时再做（`UNIFIED` §11-B 建议首版只做 chat + embedding），但**现在就不要为了"将来兼容媒体"而把 `ModelPricing` 设计成通用容器**——那会让最常用的 token 定价也变得难读。

### 附 1.4 admin 是写入口，两侧字段必须成对扩展

web 展示的每一个价格字段，admin 都必须有地方填。而 admin 的 `CatalogModel`（`frontend/admin/src/api/contracts.ts:119-128`）现在同样只有 `inputPrice` / `outputPrice` 两个：

```ts
export type CatalogModel = {
  id: string; name: string; vendor: string
  hosting: 'cloud' | 'self'; status: ModelStatus
  inputPrice: number | null      // ← 只有两个
  outputPrice: number | null
  ...
}
```

于是附 1.2 给 web 加的 `cache_read` / `cache_write` / `tiers` 等字段，**在 admin 侧没有录入口就永远是 null**——加了等于没加，web 还是只能显示两个价，`App.tsx:122` 那个 `input × 0.1` 的猜测也就没法删掉。

**因此这两处必须一起改，不能只改展示侧。** admin 的模型编辑表单要按 §3.1 的计价单元组织，且带 §3.6-④ 的提交校验（单位数量级、毛利预览、发布前的价目完整性）。

顺带一条：admin 的价格录入表单里，**输入框的单位标注要写死在 UI 上**（"USD / 1M tokens"），不要只在 placeholder 里暗示。§3.6-④ 说的单位错位是手填价格最贵的一类错误，防它最便宜的办法就是把单位摆在输入框旁边。

### 附 1.5 admin 契约：成本必须带口径标注

`api/openapi.admin.yaml` 还不存在（`ARCHITECTURE.md` §10-E）。写它时，模型分析页的 `$/1M`（`frontend/admin/src/pages/analytics/ModelsPage.tsx:105`，当前 tooltip 写的是"每百万 Tokens 的供应商计费"）必须带口径：

```yaml
        cost_per_million:      { type: number }
        cost_kind:             { type: string, enum: [vendor, amortized] }
        utilization:           { type: [number, 'null'], minimum: 0, maximum: 1 }
        breakeven_utilization: { type: [number, 'null'] }
        estimated:             { type: boolean, description: 自建当期未结束，摊销值仍为估算 }
```

这是 `SELF-HOSTED-GPU-OPERATIONS.md` §4 已经提出的三条要求（标注为摊销成本、附当期利用率、给出盈亏平衡利用率）在契约上的落点。**没有 `cost_kind`，"自建 $0.57 比云端 $0.60 便宜"这句话就是错的**——它成立的前提是利用率 80%，而那个前提不在数字里。

### 附 1.6 影响面

| 文件 | 改动 |
|---|---|
| `frontend/contracts/openapi.yaml` | `ModelPricing` 扩展；`currency` const→enum；`ModelEndpoint` 价格字段改名 + 描述；`sort=price` / `max_input_price` 补语义 |
| `frontend/web/src/api/contracts.ts:45-49, 65-66` | 同步 TS 类型（手写，需与 yaml 一起改） |
| `frontend/web/src/App.tsx:122` | **删掉 `input*.1` 的硬编码**，改读 `pricing.cache_read_per_million`，缺省显示「—」 |
| `frontend/web/src/App.tsx:84, 93` | "免费"筛选改为 `input_per_million === 0`，显式排除 null |
| `frontend/web/src/components/models/ModelRow.tsx:41-42` | null 显示为「—」（输出价已这么做，输入价的 `toFixed(2)` 会在 null 上崩） |
| `frontend/admin/src/api/contracts.ts:119-128` | `CatalogModel` 的价格字段按 §3.1 扩展（附 1.4：不改这里，web 侧加的字段永远是 null） |
| `frontend/admin` 模型编辑表单 | 按计价单元组织；单位标注写在输入框旁；接 §3.6-④ 的提交校验 |
| `api/openapi.admin.yaml`（待建） | 模型的**写**端点（创建/改价/发布），含 `effective_from` 与 `note`；成本字段按附 1.5 带口径 |
| 后端 | `catalog` handler 按 §3.4 解析价目后输出有效价，不输出内部 `same_as` 表达；**与结算共用同一个解析函数**（§3.6-②） |

**改动窗口就是现在。** 这些字段一旦有生产数据和外部 SDK 使用者，`ModelEndpoint` 的价格字段改名就变成一次破坏性版本升级。

---

## 附二：与其他文档的对接检查表

- [ ] `ARCHITECTURE.md` §4.3 的 `cost_usd_micro` 已替换为本文 §9 的三字段
- [ ] `ARCHITECTURE.md` §4.1 的 `balance_usd` 已改为 `balance_nano`
- [ ] `ARCHITECTURE.md` §6.3 的"周期性对账"已指向本文 §5.5 的三个参数
- [ ] `UNIFIED` §5.3 的 `Billing` 名是价目表的唯一计费主体键
- [ ] `UNIFIED` §6 的三阶段计费与本文 §5.1 的语义一致（异步任务以那里为准）
- [ ] `UNIFIED` §5.5 的回退边界条件表与本文 §4.3 的计费规则表逐行对应
- [ ] `SELF-HOSTED` §4 的摊销回填只写成本列，不触碰售价（本文 §4.4，需 CI 断言）
- [ ] `SERVICE` §1 的"计费不跨服务边界"仍成立：§5 全部流程发生在单一服务内
- [ ] `frontend/contracts/openapi.yaml` 的 `pricing` 已按附一的提案评审并落地
- [ ] admin 的 `CatalogModel` 与 web 的 `ModelPricing` **字段一一对应**（§3.6-①、附 1.4）
- [ ] catalog 展示价与结算价共用同一个价目解析函数（§3.6-②）
- [ ] 模型发布为 `listed`/`canary` 前校验价目完整性（§3.6-③④）
- [ ] 改价在 `audit_logs` 里标 `destructive = true`（§3.6-⑥）
