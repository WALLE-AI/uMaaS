# GPUStack 源码深度技术报告

> 版本：基于 `opensource/gpustack` @ `9650c5e5`（v2.x 主线）· 2026-09-09
> 目的：为 `SELF-HOSTED-GPU-OPERATIONS.md` 提供**可对照的工业级参考实现**
> 阅读方式：本文按"模块 → 架构 → 关键流程 → 可借鉴/不可借鉴"组织。
> 每条结论都给出源码位置，便于自行复核。

---

## 0. 为什么值得读这套代码

GPUStack 与 uMaaS 的自建 GPU 部分解决的是**同一个问题**：在一堆没有 K8s 的
GPU 机器上，把模型实例调度上去、管起来、暴露成 OpenAI 兼容接口、并计量。

它的价值不在于"抄架构"——它的形态与 uMaaS 不同（Python、单 Leader、
Higress 网关、社区产品而非商业 MaaS）——而在于**它已经踩完了一遍坑**。
本文重点提取的是那些"看起来可以简化、但代码里明确写了不能简化"的地方，
这些注释往往对应一个真实的线上事故。

代码规模（`gpustack/` 目录，仅 Python）：

| 模块 | 行数 | 职责 |
|---|---:|---|
| `worker/` | 17.8k | 节点侧 daemon：实例生命周期、指标、权重、压测 |
| `server/` | 17.8k | 控制面：控制器、调度触发、计量、Leader 选举 |
| `schemas/` | 15.8k | 数据模型（SQLModel），含状态机定义 |
| `routes/` | 27.4k | REST API |
| `policies/` | 10.1k | **调度策略：过滤器、打分器、资源适配选择器** |
| `gateway/` | 4.4k | Higress/Istio CRD 编排 |
| `scheduler/` | 3.5k | 调度主循环与资源估算 |
| `websocket_proxy/` | 3.0k | NAT 穿透隧道 |
| `gpu_instances/` | 4.2k | 云上 GPU 实例（IaaS）生命周期 |

---

## 1. 整体架构

### 1.1 三个进程角色

```
                   ┌───────────────────────────────────────┐
   客户端 ─────────►│  AI Gateway（Higress / Envoy + WASM） │
                   │  ai-proxy · model-mapper · ext-auth    │
                   └───────┬───────────────────────┬────────┘
                           │ (数据面)              │ ext-auth 回调
                           ▼                       ▼
     ┌──────────────────────────────┐   ┌────────────────────────┐
     │ vLLM / SGLang / MindIE 容器  │   │  Server（控制面）      │
     │ 直连或经 worker 反代/隧道    │   │  API · Scheduler ·     │
     └──────────────▲───────────────┘   │  Controllers · Exporter│
                    │                   │  Leader 选举           │
             docker.sock                └──────────┬─────────────┘
     ┌──────────────┴───────────────┐              │ REST + watch(SSE)
     │ Worker daemon（每节点一个）  │◄─────────────┘
     │ ServeManager · MetricExporter│
     │ ModelFileManager · Benchmark │
     └──────────────────────────────┘
```

三点值得注意：

1. **Worker 是纯执行器。** 它不做任何调度决策，也不上报"我用了哪几张卡"
   （见 §4.1）。它只上报硬件事实（`worker/collector.py`）与容器实况。
2. **Server 的调度器与控制器只在 Leader 上跑**
   （`server/server.py:1489 _start_leader_tasks`）。API Server 所有副本都跑。
3. **网关是外部件。** 路由与负载均衡交给 Higress，Server 只负责把
   "模型 → 实例地址"翻译成 McpBridge/Ingress CRD（`gateway/utils.py`）。
   Server 自身也有一条兜底代理路径（`routes/openai.py` + 简单轮询 LB），
   但那是网关关闭时的降级路径。

### 1.2 事件总线：为什么不是轮询

`server/bus.py` + `server/coordinator/`。所有资源（Model、ModelInstance、
Worker、ModelFile…）的变更都发布到 topic，订阅者用 `async for event in
X.subscribe(...)` 消费。Worker 侧则通过 HTTP watch（`clientset.X.awatch()`）
拿到同一条流。

设计上有几处很关键：

- **订阅者各自持有有界队列，满了就背压而不是丢弃**
  （`Subscriber._put_with_backpressure`）。发布路径为每个事件起独立 task，
  所以背压只卡住那一个事件的投递，不会卡住写库的调用方。
- **同 id 的 UPDATED 会被合并**（`latest_by_key`）。高频心跳不会把队列打爆。
- **DELETE 事件可能只带 id，不带行。** `Event` 的 docstring 明确写了这一点：
  跨实例的 DELETE 到达时行已经没了，无法回读。所以**每个消费者都必须处理
  两种 payload 形状**，用 `event_field()` / `resolve_event_id()` 而不是直接
  取属性。这是分布式控制面里一个很容易被忽略的细节。

### 1.3 Leader 选举：丢主就自杀

```python
# server/server.py:1474
renewed = await self._coordinator.renew_leadership(ttl)
if not renewed:
    logger.error("lost leadership, exiting for restart")
    os._exit(1)   # 绕过 cleanup，立刻死，让容器运行时拉起为 standby
```

不是"优雅停掉 leader 任务"，而是**硬退出**。理由写在注释里：防脑裂。
优雅停止意味着要保证每个控制器循环都能被可靠取消，而这在 asyncio 里
很难证明；进程级退出是唯一能证明的。

---

## 2. 调度器：过滤 + 打分的两段式

`scheduler/scheduler.py` + `policies/`。这是整个项目设计最完整的部分，
也是 uMaaS §11.2 最该对照的部分。

### 2.1 触发方式：事件驱动 + 周期全扫

```python
# scheduler/scheduler.py:102 start()
asyncio.create_task(self._schedule_cycle())          # 消费队列
scheduler.add_job(self._enqueue_pending_instances,   # 每 180s 全扫兜底
                  IntervalTrigger(seconds=180), max_instances=1)
await self._enqueue_pending_instances()              # 启动时先扫一次
async for event in ModelInstance.subscribe(
        event_types={EventType.CREATED},             # 只订 CREATED
        replay_existing=False):                      # 不回放历史
    await self._enqueue_event_instance(event.data)
```

注释里明确记录了一次事故（#4794）：原来订阅时回放全部已有实例，
导致队列被冲爆。现在改成"启动时全扫一次 + 只订 CREATED + 周期全扫兜底"。

**`_should_schedule` 里的三个超时条件**（`scheduler.py:263`）是自愈的核心：

| 条件 | 阈值 | 防的是 |
|---|---|---|
| `PENDING` 且 `worker_id is None` | 新建 或 更新超 90s | 正常入口 |
| `ANALYZING` 卡住 | > 3 分钟 | Server 在评估阶段重启 |
| `SCHEDULED` 卡住 | > 3 分钟 | 已分配但 Worker 没接手（Worker 挂了） |

即：**没有任何一个状态可以永久卡死**，每个中间态都有超时回退。

### 2.2 过滤链（硬约束）

`find_candidate()` 里固定顺序的六个过滤器（`scheduler.py:442`）：

```
ClusterFilter          → 只留目标集群的节点
GPUMatchingFilter      → 用户手选 GPU 时，只留被选中的节点
LabelMatchingFilter    → 标签选择器
StatusFilter           → 只留 READY
BackendFrameworkFilter → 引擎/驱动/CANN 版本兼容性
LocalPathFilter        → 本地路径模型：远程校验路径存在
```

`WorkerFilterChain` 有一个小而重要的设计：**每个过滤器都返回
`(通过的节点, 被过滤原因的消息列表)`**（`policies/base.py`）。这些消息最终
拼进 `ModelInstance.state_message`，用户在界面上能直接看到
"为什么没调度上去"。自研调度器最容易省掉的就是这个，然后运维只能看到
一个 Pending 而无从下手。

`LocalPathFilter` 的实现值得注意：它通过 `WorkerFilesystemClient` **实时
远程询问每个节点"这个路径存在吗"**，而不是查数据库里的缓存记录。

### 2.3 资源适配：按引擎分派的候选选择器

不同引擎的显存语义完全不同，所以选择器按引擎分派
（`scheduler.py:461`）：`VGPU / GGUF / AscendMindIE / vLLM / SGLang / Custom`。

**vLLM 选择器（`policies/candidate_selectors/vllm_resource_fit_selector.py`）
是最该细读的一个，因为它揭示了一个反直觉的事实：**

```python
# 单卡候选，vllm_resource_fit_selector.py:436
exceeds_vram = (
    self._vram_claim > gpu.memory.total * self._gpu_memory_utilization
    if self._gpu_memory_utilization > 0     # LLM
    else self._vram_claim > allocatable_vram)  # 非 LLM
exceeds_memory_utilization = (
    self._gpu_memory_utilization > 0
    and allocatable_gpu_memory_utilization < self._gpu_memory_utilization)
...
vram_claim_bytes = int(gpu.memory.total * self._gpu_memory_utilization)
```

**要点：LLM 场景下，记账的占用量是 `显存总量 × gpu-memory-utilization`，
不是"模型估算需要多少"。** 因为 vLLM 启动时会按这个比例把显存**整块吃掉**
建 KV Cache 池，它不会"用多少要多少"。

由此推出两条硬性结论：

1. **一张卡上默认放不下两个 vLLM 实例。** 默认 `gmu=0.9`，第二个实例的
   可分配比例只剩 0.1 < 0.9，直接被 `exceeds_memory_utilization` 挡掉。
   要共卡必须显式降低 gmu，且两边之和 ≤ 1。
2. **可分配显存的"比例"和"绝对值"要分别校验。** 只看绝对值够不够会漏掉
   "卡上还有 20GB，但那是 80GB 卡的 25%，达不到 gmu=0.9"这种情况。

多卡路径（`_find_single_worker_multi_gpu_full_offloading_candidates`）的策略：
按可分配显存降序排卡，逐张累加，直到 `vram_sum >= vram_claim`，
同时每一步检查 `_is_tp_size_divisible(gpu_sum)`——**TP size 必须整除注意力
头数**，包括视觉塔的头数（`get_model_vision_num_attention_heads`，
还要加上 `num_dummy_heads`）。最后跨节点候选只在**每节点 GPU 数相同**的
组合里找，因为 TP/PP 不支持异构节点。

显存估算本身在 `policies/utils.py:384 estimate_model_vram()`：

```
VRAM = 权重大小 × 1.2 + 框架开销
  1.2   ← EleutherAI transformer-math 的激活开销经验值
  框架开销 = LLM 2GiB / 非 LLM 512MiB   ← CUDA graph 捕获
  TTS 类模型系数取 3（Qwen3-TTS 实测）
  LoRA 权重单独累加
```

并留了 `GPUSTACK_MODEL_VRAM_CLAIM` 环境变量做人工覆盖——承认经验公式会失准。

### 2.4 打分：加权求和，不是字典序

```python
# scheduler.py:493
candidate_scorers = [
    PlacementScorer(model, model_instances),          # max 100
    ModelFileLocalityScorer(model, ..., max_score=5), # max 5（env 可调）
]
candidates = await CandidateScoreChain(candidate_scorers).score(candidates)
candidate = pick_highest_score_candidate(candidates)
```

`CandidateScoreChain`（`policies/scorers/score_chain.py`）在每个 scorer 前
**重置 `candidate.score = None`**，各 scorer 独立算分后累加——这样 scorer
之间不会互相污染，且顺序无关。

**PlacementScorer** 支持两种策略：

- `BINPACK`：分数 = 本次占用 / 可分配量，**占比越高分越高**。RAM:VRAM 权重
  1:2。目标是把卡填满，少开新机器。
- `SPREAD`：分数由三段构成——先看该节点上"本模型的实例数"，再看"其他模型的
  实例数"，再按 `worker:gpu = 0.85:0.15` 融合节点级与卡级分散度。
  关键在 `_score_spread_worker_score`：**如果集群里存在"本模型实例数为 0"的
  节点，就给这些节点一个 0.85 的基础分，其他节点只有 0.45**——即
  "先保证每个节点至少一个副本"优先于"整体均匀"。

**ModelFileLocalityScorer** 的分值只有 5（PlacementScorer 是 100）。
即：**权重本地性只是平局打破器，不足以推翻放置策略。** 这是一个可以商榷
的标定——本地缓存命中能省 2–5 分钟加载时间，5/100 的权重意味着几乎
永远不会因为"权重在那台机器上"而改变放置。uMaaS 若照抄这套打分，
建议把这个比例重新标定（见 `SELF-HOSTED-GPU-OPERATIONS.md` §11.2）。

### 2.5 缩容：也是打分，不是随便杀

`sync_replicas()` 发现实例数 > 期望时（`server/controllers.py:912`）：

```python
instances = await ModelInstance.all_by_field(..., for_update=True)  # 行锁
candidates = await find_scale_down_candidates(instances, model)
# 按分数升序，删最低分的 N 个
```

缩容打分链（权重来自 `envs`）：`StatusScorer(100)` → `OffloadLayerScorer(10)`
→ `PlacementScorer(1)`。含义是：**先杀不健康的，再杀 offload 程度低的，
最后才看放置**。`ModelInstanceScoreChain` 还会把各 scorer 的 max_score
归一化到统一总分，避免调权重时相对关系被破坏。

注意 `for_update=True` 的行锁——注释写明是为了和调度器抢同一批实例时
不产生竞态。

### 2.6 兼容性评估：把调度器当纯函数跑一遍

`scheduler/evaluator.py` 是个漂亮的复用：模型部署前的"能不能跑得起来"检查，
**直接调用 `scheduler.find_candidate()`**，只是不落库。

```python
# evaluator.py:229
candidate, schedule_messages = await scheduler.find_candidate(
    session, config, model, cluster_workers, cluster_model_instances)
```

带 TTL 缓存（key 是 model spec + workers 状态的 md5，且显式排除了
利用率/温度这类高频抖动字段），并用 `AsyncLimiter(50, 10)` 限流以避开
HuggingFace 的 600 RPM 限制。

**这条经验很值钱：不要为"容量评估页"写第二套计算逻辑。** 两套逻辑必然
分叉，然后界面说能跑、实际调度不上去。

---

## 3. Worker：实例生命周期

### 3.1 状态机

```
# schemas/models.py:636
PENDING → ANALYZING → SCHEDULED → INITIALIZING → DOWNLOADING → STARTING → RUNNING
   |Scheduler    |      |  ServeManager  |    Controller    | ServeManager |
   任意态 → ERROR（restart_on_error 时回到 SCHEDULED 重来）
   RUNNING → UNREACHABLE（Worker 失联）
```

Worker 状态机（`schemas/workers.py:212`）区分了三类失败，这个区分很重要：

| 状态 | 含义 | 判据 |
|---|---|---|
| `NOT_READY` | **Worker 停止上报心跳** | `heartbeat_time` 超过 150s |
| `UNREACHABLE` | **Server 连不上 Worker 的 /healthz** | 主动探测失败 |
| `MAINTENANCE` | 人工置入维护模式 | 显式标记，**跳过一切自动处置** |

心跳丢失和探测失败是两个方向的连通性，分开表达之后，报错信息可以给出
完全不同的排查建议（`compute_state()` 里两段 message 分别指向不同文档）。

### 3.2 每个实例一个子进程

`ServeManager._start_model_instance()` 用 `multiprocessing.Process` 起一个
**provisioning 子进程**，子进程内构造 `VLLMServer` 并调 `create_workload()`
创建容器。子进程存活 = "还在准备中"，退出 = "要么起来了要么失败了"
（`_is_provisioning`）。

为什么不直接在主循环里创建容器？因为拉镜像、解析模型 config、下载权重
都是长耗时阻塞操作，放主循环会拖垮心跳与其他实例的同步。

日志也由此分流：子进程的 stdout/stderr 重定向到
`{log_dir}/serve/{instance_id}.{restart_count}.log`，**带重启序号**，
所以重启后还能看上一次的失败日志。另有独立线程持续拉容器日志流
（`_persist_container_logs`），断流时用"最后 5 行作为锚点"续接，
避免 replay 导致日志重复。

### 3.3 容器参数：那些不显眼但必须设的

`worker/backends/vllm.py:340` + `base.py`：

```python
WorkloadPlan(
    host_network=True,          # 不做端口映射，直接用宿主网络
    host_ipc=self._host_ipc_enabled(),   # 只在挂共享 KV Cache 时开
    shm_size=int(shm_size_gib * 1<<30),  # 默认 10 GiB
    ...)
Container(
    restart_policy=ContainerRestartPolicyEnum.NEVER,  # ← 不用 docker restart
    execution=ContainerExecution(privileged=True, ...))
```

四个关键决定：

1. **`restart_policy=NEVER`。** 容器不自愈，由控制面按状态机重启
   （指数退避 `min(10 × 2^(n-1), 300)` 秒，`_restart_error_model_instance`）。
   理由：docker 的 restart 会绕过控制面，导致"记录说 ERROR、实际在跑"。
2. **`host_ipc` 只在必要时开**，注释写得很清楚：
   **Docker 在 host IPC 下会忽略 `shm_size`**，而且 K8s PodSecurity baseline
   直接拒绝 hostIPC。所以默认走私有 `/dev/shm` + 显式 `shm_size`。
3. **`host_network=True` + 从端口段动态分配**（`_assign_ports`，带全局锁）。
   多实例同机时固定端口必然冲突。vLLM 分布式还要额外预留：
   `ports[1]`=DP RPC(ZMQ)、`ports[2]`=PyTorch master(TCPStore)、
   `ports[3]`=VLLM_PORT，并把 `VLLM_DP_MASTER_PORT` 周围 **10 个端口整段
   拉黑**——因为 vLLM 会在这个 band 内自行占用。
4. **`--served-model-name` 强制注入**为 `model_instance.model_name`
   （`_build_command_args` 末尾的 `extend_args_no_exist`），从源头消灭
   "部署成功但调用 404"。

环境变量层面（`_get_configured_env`）：

```python
OMP_NUM_THREADS = "1"          # 避免 OpenMP 抢核，加快加载
SAFETENSORS_FAST_GPU = "1"
VLLM_CACHE_ROOT = <持久化目录>  # ★ torch.compile 缓存跨重启保留，不再重编译
NVIDIA_DISABLE_REQUIRE = "1"   # 镜像 CUDA 小版本高于宿主驱动时放行
# 分布式必设：
NCCL_SOCKET_IFNAME = "=<ifname>"   # 前导 '=' 是精确匹配语法
GLOO_SOCKET_IFNAME = "<ifname>"
VLLM_HOST_IP = <worker.ip>
```

`VLLM_CACHE_ROOT` 这条对冷启动时间影响很大——CUDA graph 捕获与
torch.compile 的产物如果每次重启都重算，会多出 30–90 秒。

还有一个产品决策：**`--max-model-len` 若模型声明超过 8192，自动压到 8192**
（`_build_max_model_len_arguments`），除非用户显式指定。理由是默认按模型
最大上下文开 KV Cache 会让显存需求爆炸，而绝大多数请求用不到。

### 3.4 健康检查：三层

```
1. 容器状态          workload.state ∈ {PENDING, INITIALIZING} → 还在起，跳过
2. HTTP 探针         GET /v1/models（vLLM/SGLang 默认）或引擎自定义路径
3. 真实推理探针      POST /v1/chat/completions {"max_tokens":1}
```

第三层（`is_inference_ready`）默认关闭，按模型用
`GPUSTACK_MODEL_INFERENCE_HEALTH_CHECK_ENABLED` 打开，可配 interval /
timeout / failure_threshold。按模型类别选不同端点：embedding 打
`/v1/embeddings`，reranker 打 `/v1/rerank`，图像/语音类直接跳过。

**一个很聪明的优化**（`sync_model_instances_inference_health`）：

```python
# 如果最近 interval 内有过成功的真实推理，跳过主动探针
last_success = self._last_successful_inference.get(mi.id, 0)
if last_success > now - interval:
    self._inference_health_check_failures.pop(mi.id, None)
    continue
```

真实流量本身就是最好的健康证明。`record_successful_inference` 由 worker 的
反向代理在响应成功时回调。这样忙碌的实例不会被探针额外打扰，
而空闲实例仍然被持续验证。

### 3.5 孤儿容器对账

`worker/workload_cleaner.py`，每 120 秒一轮：

```python
current = {实例/压测/缓存服务 的 deployment_metadata.name}   # 从 Server 拉
for w in list_workloads():                                   # 从容器运行时拉
    if w.name not in current and 容器创建时间超过 300s 宽限期:
        delete_workload(w.name)
```

**宽限期是必须的**：容器刚被创建、Server 侧记录还没写完的窗口里，
不加宽限期会把正在创建的实例误杀。压测容器额外多一条规则——
FAILED/INACTIVE 状态的直接清理，不管在不在名单里。

反方向的对账在 `ServeManager.sync_model_instances_state()`，
这段代码的注释是整个项目里最值得读的一处：

```python
# 1. 先快照本地状态，再拉列表 —— 顺序反了会和 CREATED 事件竞态，
#    把刚分配来的实例当成陈旧的杀掉
local_assigned_ids = {...}
response = self._clientset.model_instances.list()      # 读 watch 缓存，O(1)
reap_ids = local_assigned_ids - {缓存里属于本节点的}

# 2. 缓存不是权威：awatch 重连时会先清空缓存再置 _watch_started，
#    这个窗口里读到的是"空但看起来权威"的缓存，会把健康实例当陈旧
if reap_ids or force_authoritative:
    response = self._clientset.model_instances.list(
        params={"page": -1}, use_cache=False)          # 权威回读
    reap_ids = local_assigned_ids - {...}
for stale_id in reap_ids:
    self._stop_model_instance(stale, delete_logs=True)
```

**结论：任何会摧毁存活工作负载的判定，都必须基于一次权威的、
不分页的、绕过缓存的回读。** 快路径可以用缓存（常态零成本），
但真要动手之前必须复核一次。这是自研调谐循环最容易出的事故。

同理，DELETED 事件到达时**不直接拆除**：

```python
# _dispatch_model_instance_event
if event.type == EventType.DELETED:
    # 交给周期性 reap 处理。这里也拆的话，会有两个并发调用方
    # 竞争 delete_workload。而且 reap 本来就必须能处理
    # watch 断连期间丢失的 DELETED，所以让它一并处理即可。
    return
```

**只留一条拆除路径。** 事件是加速器，不是唯一入口。

---

## 4. GPU 记账：单一真相源

这是 uMaaS §11.3 最该直接借鉴的一节。

### 4.1 已分配量从"绑定关系"推导，不从节点上报

```python
# policies/utils.py:45
def compute_worker_allocated(all_model_instances, worker_id, gpu_type=None) -> Allocated:
    """Aggregate (ram, {gpu_index: vram}) for worker_id from current
    ModelInstance assignments — main worker + distributed subordinates.

    The single source of truth ... Mirrors the K8s scheduler pattern of
    deriving Allocated from current workload→node bindings rather than
    from anything the node self-reports."""
```

而且他们**主动废弃了节点上报的字段**：

```python
# schemas/workers.py:52
allocated: Annotated[Optional[int], deprecated(
    "Allocated is computed server-side on each /v2/workers query from the
     current set of ModelInstance bindings; this field on the worker-reported
     status is no longer populated and should not be read directly.")]
```

即：`gpus.allocated` 不是一个被写入的状态，而是一个**从 deployments 表
派生的视图**。这样"记账漂移"这个故障类别在结构上就不存在了——
没有第二份账可以漂。

可分配量的计算（`get_worker_allocatable_resource`）：

```
allocatable.vram[i] = max(gpu[i].total - allocated.vram[i] - system_reserved.vram, 0)
allocatable.ram     = max(mem.total - allocated.ram - system_reserved.ram, 0)
# 统一内存（Apple Silicon 等）额外扣一次，且 vram[0] 取 min(ram, vram[0])
```

`system_reserved` 是每节点可配的预留，默认给系统留出余量。
**自研调度器最常见的漏项就是这个**——不留预留，节点会在 OOM killer
和调度器之间来回震荡。

### 4.2 那"实际占用"呢？

实际占用（`nvidia-smi` 语义）走另一条路：Worker 上报
`gpu.memory.used`，导出为 `worker_node_gram_used_bytes`；
而绑定推导出的量导出为 `worker_node_gram_allocated_bytes`
（`worker/exporter.py`，同样调 `compute_worker_allocated` 保证算法一致）。

**两条指标的差值就是对账信号。** 不需要额外写一个"对账任务"：
`gram_used > gram_allocated + 阈值` 就是有人在外面偷偷用卡；
`gram_allocated > gram_used + 阈值` 就是记录有幽灵。做成告警规则即可。

### 4.3 卡的选择永远在 Server 侧

`_get_selected_gpu_devices()`（`worker/backends/base.py:501`）只做一件事：
把 `model_instance.gpu_indexes`（Server 写的）翻译成设备。
Worker 从不自己挑卡。传给容器的方式：

```python
resources[runtime_key] = ",".join(str(d.index) for d in gpu_devices)
# 对 NVIDIA 最终落成 NVIDIA_VISIBLE_DEVICES / --gpus device=0,1,2,3
```

并且**校验所选卡必须同型号**，否则直接 `RuntimeError`——
混型号 TP 会以极其难查的方式失败。

---

## 5. 调谐循环：一个正确的 WorkQueue

`server/workqueue.py` 是 client-go `RateLimitingInterface` 的 Python 移植。
写自研 controller 的话，这个文件应当逐行读。它的模块 docstring 直接
说明了动机：

> in-flight reconciles previously re-observed downstream state by writing
> their own status to the DB, which re-fired the event bus and re-enqueued
> the item — a self-perpetuating poll loop.

**"控制器写自己的 status → 触发事件 → 再次入队"这个自激循环**，
是所有自研调谐循环的必经之坑。解法是 `add_after()`：在内存里定时重入，
不碰数据库。

五条语义保证：

| 保证 | 实现 | 防的是 |
|---|---|---|
| **同 key 串行** | `_processing` 集合，`done()` 才放行 | 两个 goroutine 同时改一个部署 |
| **DELETED 优先且黏性** | 插队头 + 取消延迟项 + 不被后到的非 DELETE 覆盖 | 删除被更新事件挤掉 |
| **延迟堆** | 最小堆，消费者精确等到下一个到期时刻 | 轮询空转 |
| **去抖窗口 + 最大等待** | `min(now+window, first_seen+max_wait)` | 连续更新流饿死处理 |
| **按 key 指数退避** | `min(base·2^n, cap)` + jitter，`forget()` 重置 | 失败项打爆队列 |

并发模型写得很明确：**单事件循环、单消费者调 `get()`**，生产者全部同步
不 await，所以队列内部完全不需要锁。消费者按 key 起 task，
`done()` 在 task 结束时调用——从而做到"跨 key 无限并发、同 key 严格串行"。

### 5.1 三层调谐，缺一不可

综合全项目，GPUStack 的调谐是三层叠加的：

| 层 | 周期 | 作用 |
|---|---|---|
| **事件驱动** | 毫秒 | 快路径，正常情况下一切靠它 |
| **周期全扫** | 180s（调度）/ 15s（实例状态）/ 120s（孤儿） | 兜底事件丢失、进程重启 |
| **权威回读** | 仅在"要做破坏性动作"时 | 防缓存不一致导致误杀 |

只做第一层的系统在 watch 断连时会静默失效；只做第二层的系统延迟高；
少了第三层的系统会周期性地误杀健康实例。

---

## 6. 可观测性

### 6.1 双层指标，且引擎指标要做名字归一

Worker 的 `/metrics` 同时导出两类：

- **硬件层**（`worker/exporter.py`，来自 `WorkerStatusCollector` →
  `gpustack_runtime` 检测器）：利用率、显存、温度、功耗、文件系统水位。
- **引擎层**（`worker/runtime_metrics_aggregator.py`）：每 3 秒抓一次本节点
  所有 RUNNING 实例的 `/metrics`，归一化后缓存。

**归一化才是重点。** `assets/metrics_config/metrics_config.yaml` 是一张
**带版本区间的映射表**：

```yaml
runtime_mapping:
  vLLM:
    "*":                                          # 当前版本
      vllm:num_requests_waiting:  gpustack:num_requests_waiting
      vllm:kv_cache_usage_perc:   gpustack:kv_cache_usage_ratio
      vllm:num_preemptions:       gpustack:request_preemptions
      vllm:request_time_per_output_token_seconds: gpustack:time_per_output_token_seconds
    "<0.11.1":  vllm:num_requests_swapped: ...
    "<0.11.0":  vllm:time_per_output_token_seconds: ...      # ← 0.11.0 改过名
    "<0.9.2":   vllm:gpu_cache_usage_perc: gpustack:kv_cache_usage_ratio  # ← 0.9.2 改过名
  SGLang:
    "*":  sglang:num_queue_reqs: gpustack:num_requests_waiting
          sglang:num_retracted_requests: gpustack:request_preemptions
          sglang:token_usage: gpustack:kv_cache_usage_ratio
```

**vLLM 至少改过两次核心指标名**：`gpu_cache_usage_perc` →
`kv_cache_usage_perc`（0.9.2）、`time_per_output_token_seconds` →
`request_time_per_output_token_seconds`（0.11.0）。任何直接把 vLLM 原始
指标名写死进告警规则、路由逻辑或看板的系统，都会在升级引擎的那天
静默失效——**看板会显示为一条平线，而不是报错**。

版本从哪来？`fetch_and_update_api_backend_version` 会去实例的 API
问一次真实版本，写回 `model_instance.api_detected_backend_version`——
不信配置里写的版本，信实例自己报的。

同时导出两个端点：`/metrics`（归一化）与 `/metrics/raw`（原样透传 +
补上 gpustack 标签）。归一化用于告警与路由决策，raw 用于深度排障。

### 6.2 Prometheus 的服务发现：http_sd

裸机集群的节点是动态的，静态 `targets` 列表不可维护。GPUStack 让
**Server 自己当服务发现源**：

```yaml
# docker-compose/prometheus/prometheus.yml
- job_name: gpustack-worker-discovery
  http_sd_configs:
  - url: 'http://gpustack-server:10161/metrics/targets'
    refresh_interval: 1m
```

`/metrics/targets`（`exporter/exporter.py:385`）返回按集群分组、
**只包含 READY 且 metrics_port > 0 的节点**，并打上 `cluster_id` /
`cluster_name` 标签。另有 `/metrics/proxy-targets` 给隧道模式的节点用
（走 Server 侧代理地址）。

这一条几乎是零成本的：控制面本来就有权威的节点表。

---

## 7. 权重分发

`schemas/model_files.py` + `worker/model_file_manager.py` +
`server/controllers.py` 的 `ensure_instance_model_file`。

模型文件是**每（节点 × 模型源）一行**的一等资源，有自己的状态机
（`DOWNLOADING / READY / ERROR`）、进度、`resolved_paths`、`size`。

流程：

```
实例进入 INITIALIZING
  → 控制器 ensure_instance_model_file()：为主节点（及需要下载的从节点）
    创建 ModelFile 行，state=DOWNLOADING
  → 各节点的 ModelFileManager watch 到属于自己的行，
    投进 ProcessPoolExecutor（并发上限 min(max_concurrent_downloads, cpu_count)）
  → 下载进度通过劫持 tqdm 回写（_handle_tqdm_update）
  → 完成置 READY，实例才能继续到 STARTING
```

几个细节：

- **下载在独立进程池里做**，不占事件循环，且能被取消（`cancel_flag`）。
- **删除时要清理 `.incomplete` / `.lock` / `.metadata` 残留**
  （`_get_incomplete_model_files`），HuggingFace 的临时文件名是按 etag
  编码的，得反查 etag 才能定位。这类清理不做，磁盘会慢慢被吃光。
- 尺寸估算优先用已 READY 的 ModelFile 记录的 `size`
  （`_get_cached_model_size`），避免每次调度都去打 HF API。
- 调度时 `prioritize_workers_with_model_files()` 把已有文件的节点排到前面
  ——注意这是**给远程读 config 用的优化**，与打分环节的
  `ModelFileLocalityScorer` 是两件事。

**GPUStack 没有做的：** 没有对象存储 + 节点本地 LRU 缓存这一层。
它直接从 HF/ModelScope/本地路径拉。uMaaS §5 设计的
"对象存储权威副本 + 节点 SSD 缓存 + LRU 淘汰"比它更完整，
但也因此多出"淘汰时不能动正在被引用的权重"这个 GPUStack 不需要处理的问题。

---

## 8. 计量与成本

### 8.1 GPU 机时计量：带水位线的幂等累加

`server/resource_usage_collector.py` 的模块 docstring 值得整段读。核心：

```
resource_events（相位变更事件）
  → 每个实例维护一个"当前打开的计量窗口"
  → 窗口关闭时，把经过的秒数按 UTC 整点切片
  → 写入 metered_usage：一行 = (实例, 小时, 计费形态)
     quantity  = 墙钟秒数（整机 SKU，不乘卡数）
     sku_count = 卡数（切片卡可为小数）
     ⇒ GPU-Hours = SUM(quantity × sku_count)
```

**幂等性靠行上的 `settled_until` 水位线：**

> A settlement only adds the slice of an hour-segment *after* the row's
> `settled_until`, so re-processing the same window (event replay, tick
> overlap, restart, stop→start within an hour) never double-counts — the
> durable cursor lives on the row, not only in memory.

游标落在**数据行上**而不只在内存里。事件重放、tick 重叠、进程重启、
同小时内 stop→start，全部天然幂等。uMaaS §4 的"摊销回填"任务需要
完全相同的机制——回填是周期性重跑的，没有水位线就会重复累加。

另外两条：

- **`sku` 是运行时实例类型的身份快照**（`type_snapshot`），不是派生字符串。
  查类型行时**忽略 `deleted_at`**——机型可以下线，但下线时还在跑的实例
  仍要按原机型计费。这正是那张表要软删的原因。
- **算不出计费份额的窗口不结算**，留到下个 tick 重试。注释写明：
  "falling back to a whole card would silently overcharge"。
  **宁可延迟计费，不可默认多收。**

### 8.2 请求级用量：审计行不设外键

`schemas/model_usage_details.py` 的 docstring：

> Reference id columns are plain integers, **not** foreign keys. Audit rows
> must outlive the entities they describe; losing the historical id (which
> `SET NULL` would do on parent delete) is a worse audit outcome than losing
> the live join, so ids stay as reported and `*_name` columns hold mutable
> display snapshots alongside.

并且明确 `ModelUsage`（按天聚合，供看板）与 `ModelUsageDetails`
（每请求一行，供对账/审计）**不是 1:1**，来自同一条摄入路径但服务不同读模式。
uMaaS 的 `BILLING-AND-PRICING.md` 已有类似分层，这条可以互相印证。

---

## 9. 压测与容量：SLO 驱动的自动爬坡

`schemas/benchmark.py` + `worker/benchmark/{runner,analysis}.py`。
这是全项目"产品判断"最密集的一块。

### 9.1 三种负载形态、两种负载轴

```
负载轴：fixed_rate（开环，按 rps 施压）| concurrency（闭环，固定在途数）
负载形态：auto_tune（自适应爬坡）| stages（逐档跑）| single（单点）
```

`BenchmarkLoadTypeEnum` 被做成枚举而非字符串，注释解释了原因：
曾经有个拼写错误让标着 "concurrency" 的跑变成了 rate 扫描，静默错了很久。

### 9.2 SLO 阈值：3 指标 × 3 聚合

`SLO_THRESHOLDS` 是 9 条 `(字段, 指标, 缩放, CLI flag, 回退指标)` 的
单一真相源，被 runner 和 analysis 共同消费。原来两边各抄一份，
"加一个聚合就得改好几个列表，漏掉哪个就静默丢失阈值"。

**其中一条注释是这份代码里最有价值的一段：**

> TPOT thresholds bound the DECODE-ONLY per-token time, which guidellm files
> under `inter_token_latency_ms` — (last_token − first_token) / (tokens − 1).
>
> They used to bound `time_per_output_token_*` alone, which is guidellm's
> OTHER per-token metric: (last_token − request_start) / tokens, i.e. **TTFT
> folded into the decode average**. That charged prefill and queue wait to the
> decode loop; the error is TTFT / (n · TPOT), so ~5% on a 128-token run and
> **~40% at 16 output tokens**, and it grew with load exactly where the SLO
> decides capacity.

即：**"每 token 时间"有两个互相冲突的定义**，用错的那个会把 TTFT 摊进
解码时间，短输出下误差 40%，而且误差随负载增大——恰好在决定容量的
那个区间最不准。任何做容量基准的系统都必须显式选定并标注用的是哪个。

回退逻辑也很讲究：服务端不做增量流式（一个 chunk 吐完整个输出，
低负载下常见）时，首末 token 时间戳相同，`inter_token_latency` 报 0。
这时回退到 `time_per_output_token`——**因为 0 会通过任何阈值**，
不回退则该点被判失败，爬坡在第一个点就被卡死。

### 9.3 结论的可信度判定

`analysis.py` 把"测出来的曲线"和"爬坡引擎自己说的停止原因"分开处理：

- **facts**：runner 写的 `{id}__ramp.json` 说明**为什么停**
  （`capacity_plateau` / `upper_bound` / `budget_points` / `budget_seconds`）。
  几种终止会留下**完全相同的曲线**，下游无法反推，所以必须由 runner 记录。
- **grid**：测点本身，用来判断结论是否可信、用户该改什么。

阈值常量（都带注释解释为什么是这个数）：

```python
MIN_SUCCESS_RATE = 0.95      # 成功率低于此 → 该点已过载，不可信
PEAK_PLATEAU_TOLERANCE = 0.05 # 最后一档吞吐涨幅 <5% → 曲线已平，不是范围不够
SLO_HEADROOM_RATIO = 0.7     # 只用掉 70% 预算 → 停止原因不是延迟
MIN_KEEPUP_RATIO = 0.8       # 实际 rps < 80% 施压 rps → 明显跟不上
```

并会输出"边界警告"：**最优点落在测量区间端点上时，这个结论是个下界
而不是拐点**（`slo_boundary_located`：EDGE vs FLOOR）。

自研压测最常见的错误就是给出一个孤零零的"最大并发 = 64"，
不说明这是测出来的拐点、还是扫描范围的上限。

---

## 10. 网络：NAT 后的节点怎么接入

`websocket_proxy/`。GPUStack 有三种 `proxy_mode`：

| 模式 | 网关上游指向 | 适用 |
|---|---|---|
| `DIRECT` | 实例地址 `worker_ip:port` | 同网段 |
| `WORKER` | Worker 的 API 端口（Worker 做反代） | 实例端口不暴露 |
| `TUNNEL` | **Server 侧代理地址** | **节点在 NAT 后 / 无入站** |

TUNNEL 的机制：

```
Worker 启动 → MessageClient 建立到 Server 的 WebSocket 长连接
             携带自己的 CIDR（{worker_ip}/32）与 unix socket 列表
Server 侧    → 用 Patricia/radix trie 做最长前缀匹配，登记 client
             → 分配一个本地 HTTP 代理地址，写回 worker.proxy_address
             → 网关的 McpBridgeRegistry 指向这个代理地址
请求到来     → 代理发 CONNECT_REQUEST 到 Worker
             → Worker 主动连本地目标，回 CONNECT_RESPONSE
             → 数据在这条 WS 上双向流动
```

重连是指数退避 + 抖动（1s → 60s，jitter 30%）。IP 变化时
`update_cidrs()` 会**主动关闭连接触发重连**，让 Server 重新登记——
比设计一个"更新 CIDR"的消息类型简单得多。

Server 侧还支持 federation（多 Server 互相同步 client 注册表），
这样任一 Server 收到请求都能找到隧道。

**对 uMaaS 的意义**：§10.5 已定"Agent 一律主动外联"，方向是对的。
GPUStack 补充的关键细节是：**网关的上游注册项必须指向 Server 侧代理地址，
而这个地址只在隧道连上之后才存在**（`_worker_tunnel_proxy_registry` 在
`proxy_address is None` 时返回 `None`）。所以"隧道断开"要能自然表达为
"这个上游从网关配置里消失"，而不是"上游还在但连不上"。

---

## 11. 云上 GPU 实例：另一套生命周期

`gpu_instances/` + `cloud_providers/`。GPUStack v2 支持直接开云上 GPU 实例
（Kubernetes CR 或 DigitalOcean/Shuihua 等云厂商）。

相位（`GPUInstancePhase`）比模型实例更细，且分了三个集合：

```python
FAILED_PHASES        = {CreateFailed, SSHPublicKeyCreateFailed,
                        PersistentVolumeTypeCreateFailed,
                        PersistentVolumeCreateFailed, InitializeFailed}
TRANSITIONING_PHASES = {Deleting, Stopping, Starting, NotReady}  # 用户操作被门禁
INTERRUPTED_PHASES   = {Stopped}                                  # 可恢复
```

三条设计经验：

1. **失败相位要按"失败在哪一步"分开**，不是一个统一的 `Failed`。
   SSH key 建失败和 PV 建失败的处置完全不同。
2. **过渡态要门禁用户操作**：`Stopping` 中不允许再点 `/stop`。
   这个集合被路由层和控制器共同引用，避免两边各写一份判断。
3. **`is_failed()` 用 `phase.endswith("Failed")` 宽判**，因为下游 CR 可能
   报出 GPUStack 没定义的失败相位——**对未知失败要宽容，对已知失败要精确**。

`WorkerPool` 则是"按副本数自动开机器"的抽象（`new_workers_from_pool`），
带 `batch_size` 限制同时开机数量。注释里记了一个 bug：`batch_size` 为
`None` 时和 0 比较抛 `TypeError`，被外层吞成"reconcile 失败"，
结果**池子永远不创建机器，日志里只有一行**。这类"异常被吞导致静默不工作"
是控制器最难查的故障。

---

## 12. 定时伸缩：cron 窗口

`server/scaling_scheduler.py`，语义对齐 GCP scaling schedule / KEDA Cron scaler：

```
每条规则 = (start_cron, duration_seconds, replicas)
窗口在 start_cron 触发时打开，持续 duration_seconds
窗口重叠时：最近开启的赢；同时开启的取 replicas 较大者（与规则顺序无关）
所有窗口之外：回落到 baseline_replicas
```

它**只设置期望副本数**，实际扩缩交给已有的 `sync_replicas` 调谐——
职责分离得很干净。

两个时间处理细节：

- cron 在**平台统一业务时区**里求值（不是每模型时区），
  与用量 rollup 共用 `resolve_rollup_tz()`。
- 窗口结束时间用**绝对时间**相加（先转 UTC 再加 timedelta），
  不是墙钟相加——否则跨夏令时窗口会莫名伸缩一小时。
- 已知缺陷坦白写在 docstring 里：DST 回拨那一小时窗口可能读作已关闭。

**GPUStack 没有做基于指标的自动伸缩。** 只有 cron。这是一个有意的取舍：
模型实例的扩容要 3–8 分钟，指标驱动的反馈环在这个时间常数下容易震荡。

---

## 13. 网关侧：加权分流与 fallback

`gateway/utils.py` + `schemas/model_routes.py`。

`ModelRoute` 是对外的模型名，`ModelRouteTarget` 是它的后端，
每个 target 要么指向本地 `model_id`，要么指向外部 `provider_id`，
带 `weight`、`overridden_model_name`、`fallback_status_codes(4xx|5xx)`、
`state(active|unavailable)`。

权重落到 Ingress 上要转成整数百分比，用的是**汉密尔顿最大余额法**
（`hamilton_calculate_weight`）：先按最小公倍数缩放使得"每个 target 的
权重能被其实例数整除"，再按精确配额取整、按余数大小分配剩下的席位，
保证总和恰好 100。

这套设计与 uMaaS `UNIFIED-PROVIDER-INTERFACE.md` §5.5 的连续评分是
**两条不同的路线**：

| | GPUStack：静态加权 + fallback | uMaaS：连续评分 |
|---|---|---|
| 决策位置 | 网关（Envoy），零额外延迟 | 网关进程内评分 |
| 溢出触发 | 状态码匹配（4xx/5xx）后 fallback | 分数连续下降，自然让位 |
| 自建饱和 | **看不见**——排队不产生错误码 | 队列深度进 speed 因子 |
| 运维负担 | 权重要人工调 | 自动 |

**GPUStack 这套在"自建过载"场景下是失灵的**：vLLM 过载时不会返回 5xx，
它只是排队，TTFT 线性劣化。基于状态码的 fallback 永远不会触发。
这恰好印证了 uMaaS §2 的判断——**溢出必须由队列深度驱动，不能由错误码驱动**。

反过来，GPUStack 的做法有一个 uMaaS 该借鉴的点：
**把"对外模型名"和"后端实例"显式分成两张表**，中间那层
`overridden_model_name` 解决的正是 §7 提到的
`--served-model-name` 与上游模型名不一致的问题——它不是靠约定，
而是靠一个可编辑的字段。

---

## 14. 引擎版本与镜像解析

`worker/backends/base.py:1140 _resolve_image()`。优先级：

```
1. model.image_name（用户显式指定）
2. InferenceBackend 配置表里按 backend_version 查到的镜像
3. 按 (设备后端, 变体, 引擎, 引擎版本, 平台架构) 从 gpustack-runner 目录解析
```

第 3 条里有一个关键动作：候选镜像可能针对不同 CUDA 版本编译，
用 `pick_runtime_version(候选版本列表, 宿主运行时版本)` 选——
规则是"取 ≤ 宿主的最新版；否则同大版本里取最旧"。

以及 `_should_disable_cuda_compat()`：当镜像的 CUDA 小版本高于宿主驱动
（同大版本）时，注入 `NVIDIA_DISABLE_REQUIRE=1` 跳过容器工具链的
`NVIDIA_REQUIRE_CUDA` 检查——注释说明是因为 cuda-compat 在消费级卡上
会直接把容器搞挂。默认关闭，按模型或全局开关打开。

**这一整块是 uMaaS 目前完全空白的：** §11.4 里的
`vllm/vllm-openai:v0.9.0` 是写死的一行。真实运营中会出现
"新模型需要新 vLLM、老模型在新 vLLM 上跑不起来"的情况，
镜像版本必须是**部署配置的一等字段**，而不是常量。

---

## 15. 值得借鉴 vs 不建议照抄

### 15.1 直接可借鉴（高价值）

| # | 做法 | 位置 |
|---|---|---|
| 1 | **已分配显存从绑定关系派生，节点上报字段废弃** | `policies/utils.py:45` |
| 2 | **vLLM 的显存 claim = 卡总量 × gmu**，且校验可分配"比例" | `vllm_resource_fit_selector.py:436` |
| 3 | **破坏性动作前必须权威回读（绕缓存、不分页）** | `serve_manager.py:535` |
| 4 | **只留一条拆除路径**，DELETED 事件不直接拆 | `serve_manager.py:1207` |
| 5 | **孤儿容器清理要有创建时间宽限期** | `workload_cleaner.py` |
| 6 | **容器 restart_policy=NEVER**，重启由控制面按退避执行 | `vllm.py:233` |
| 7 | **引擎指标名要做版本感知的归一化** | `metrics_config.yaml` |
| 8 | **Prometheus 用 http_sd，控制面当发现源** | `exporter/exporter.py:385` |
| 9 | **健康检查三层，且真实流量可豁免主动探针** | `serve_manager.py:1068` |
| 10 | **计量幂等靠行上的 `settled_until` 水位线** | `resource_usage_collector.py` |
| 11 | **算不出计费份额就不结算，宁延后不多收** | 同上 |
| 12 | **容量评估 = 调度器 dry-run，不写第二套逻辑** | `scheduler/evaluator.py` |
| 13 | **TPOT 有两个定义，用错误差可达 40%** | `schemas/benchmark.py:145` |
| 14 | **压测结论要区分"拐点"与"扫描上限"** | `benchmark/analysis.py` |
| 15 | **过滤器返回被过滤原因，拼进 state_message** | `policies/base.py` |
| 16 | **`host_ipc` 会让 Docker 忽略 `shm_size`** | `backends/base.py:569` |
| 17 | **`VLLM_CACHE_ROOT` 持久化，省掉每次重编译** | `vllm.py:386` |
| 18 | **分布式必须钉 `NCCL_SOCKET_IFNAME`（前导 `=`）** | `vllm.py:501` |
| 19 | **WorkQueue：同 key 串行 + DELETE 优先 + 按 key 退避** | `server/workqueue.py` |
| 20 | **丢主即 `os._exit(1)`，不做优雅退出** | `server/server.py:1474` |

### 15.2 需要改造后再用

- **打分权重标定**：locality 只有 placement 的 5%。uMaaS 的权重可能在
  共享存储上，本地缓存命中省的时间更多，这个比例要重标。
- **调度队列是单消费者串行的**（`_schedule_cycle` 一次处理一个）。
  节点少时没问题，规模上来后是明显瓶颈。
- **`sync_replicas` 每次事件都全量对比实例数**，在模型多时会有 N 次查询。

### 15.3 不建议照抄

- **状态码驱动的 fallback**（§13）：对自建实例的排队式过载完全失灵。
- **没有指标驱动的伸缩**：GPUStack 只有 cron。uMaaS 有溢出到云端 API 的
  兜底路径，时间常数问题没那么严重，可以做基于队列深度的预测性扩容。
- **权重直接从 HF/ModelScope 拉**：没有对象存储 + 本地缓存层。
  uMaaS §5 的设计更完整。
- **`privileged=True`**：GPUStack 对所有推理容器都开特权。
  这在多租户环境下不可接受，应当收敛到具体的 capability 与设备挂载。

---

## 16. 一句话总结每个模块

| 模块 | 一句话 |
|---|---|
| `scheduler/` | 过滤 → 按引擎分派的资源适配 → 加权打分 → 取最高分；全程可解释 |
| `policies/` | 硬约束是过滤器，软偏好是打分器，两者不可混用 |
| `worker/serve_manager` | 每实例一子进程；状态同步的每个破坏性分支都先权威回读 |
| `worker/backends/*` | 把"模型 + 节点 + 卡"翻译成一份完整的容器规格，包括那些不显眼的 env |
| `server/controllers` | 期望态（replicas）与实际态（instances）的差集处理，事件驱动 |
| `server/workqueue` | client-go 工作队列的正确移植：同 key 串行、DELETE 优先、按 key 退避 |
| `server/bus` | 有界队列 + 背压 + 同 id 合并；DELETE 事件可能只有 id |
| `policies/utils` | GPU 记账的单一真相源：从绑定关系派生，不信节点自述 |
| `worker/exporter` + `metrics_config` | 双层指标 + 版本感知的名字归一化 |
| `benchmark/` | SLO 驱动的自动爬坡，且诚实说明结论的可信边界 |
| `resource_usage_collector` | 带持久水位线的幂等机时累加 |
| `websocket_proxy` | NAT 后节点主动外联，网关上游指向 Server 侧代理 |
| `gateway/` | 把模型路由翻译成 Higress CRD；加权分流用汉密尔顿法取整 |

---

## 附：复核入口

```bash
cd opensource/gpustack/gpustack

# 调度全链路
sed -n '424,520p' scheduler/scheduler.py          # find_candidate
sed -n '400,480p' policies/candidate_selectors/vllm_resource_fit_selector.py

# GPU 记账
sed -n '45,150p'  policies/utils.py

# 调谐与反熵
sed -n '480,600p' worker/serve_manager.py         # 权威回读
cat worker/workload_cleaner.py                    # 孤儿清理
cat server/workqueue.py                           # 工作队列

# 容器规格
sed -n '207,360p' worker/backends/vllm.py
sed -n '569,830p' worker/backends/base.py

# 可观测
cat gpustack/assets/metrics_config/metrics_config.yaml
sed -n '385,420p' exporter/exporter.py

# 计量
sed -n '1,45p'    server/resource_usage_collector.py

# 压测
sed -n '135,196p' schemas/benchmark.py            # SLO_THRESHOLDS 及那段注释
sed -n '1,64p'    worker/benchmark/analysis.py
```
