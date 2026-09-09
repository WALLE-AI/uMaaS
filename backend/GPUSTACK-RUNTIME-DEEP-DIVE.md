# gpustack-runtime 源码深度技术报告

> 版本：基于 `opensource/runtime` @ `55bbd00` · 2026-09-09
> 定位：`gpustack-runtime` 是 GPUStack 的**硬件抽象层**——被 `gpustack.worker`
> 以 `from gpustack_runtime.detector import ...` / `from gpustack_runtime.deployer import ...`
> 直接依赖。上一份报告（[`GPUSTACK-DEEP-DIVE.md`](./GPUSTACK-DEEP-DIVE.md)）讲的是
> 调度与编排，这一份讲的是**它下面那一层：GPU 怎么被发现、怎么被交给容器**。
> 服务对象：`SELF-HOSTED-GPU-OPERATIONS.md` §11.3（GPU 记账）与 §11.4（实例生命周期）。

---

## 0. 为什么这个包比上层更值得抄

上层（调度、控制器、计量）是业务逻辑，各家形态不同，抄的是**思路**。
而这一层是**和硬件/驱动打交道的胶水**，各家面对的是同一批驱动、同一批坑。
这里的每一条特殊处理，背后都是一个具体型号、一个具体驱动版本、一个具体 issue。

仓库里还留着一份完整的设计文档 `specs/2026-08-14-detector-alignment-and-workload-exit-status.md`
（1088 行），逐厂商记录了"我们和参考实现哪里不一样、为什么"。**这份 spec 本身就是
最有价值的产出**——它把"这个 if 为什么在这里"全部写下来了，而这正是自研 GPU
管理层三个月后没人敢改的原因。

规模：

| 模块 | 行数量级 | 职责 |
|---|---:|---|
| `detector/` | ~9 万字符 × 9 厂商 | 设备发现：清单、用量、拓扑 |
| `detector/py*/` | 手写 ctypes 绑定 | pydcmi(华为) / pymxsml(沐曦) / pycndev(寒武纪) … |
| `deployer/` | ~40 万字符 | 把"要几张卡"翻译成 Docker / Podman / K8s 的具体参数 |
| `deployer/cdi/` | ~6 万字符 | 为没有官方 container toolkit 的厂商自己生成 CDI spec |
| `cmds/` | ~4 万字符 | `gpustack-runtime detect / topology / deploy` CLI |

支持九家：NVIDIA、AMD、华为昇腾、寒武纪、海光、天数智芯、沐曦、摩尔线程、平头哥。

---

## 1. 核心抽象：Device / Topology / Detector

### 1.1 `Device` 的字段划分是有讲究的

`detector/__types__.py:154`。表面看是一个平铺的 dataclass，实际上字段被**严格分成两类**，
这个划分是整个模块设计的支点：

```python
# 清单（information）—— 慢变，一次检测即可，可缓存
manufacturer, index, name, uuid,
driver_version, runtime_version, runtime_version_original,
compute_capability, cores, memory, power, appendix

# 用量（usage）—— 快变，每次刷新都要重新读
cores_utilization, memory_used, memory_utilization,
temperature, power_used
# memory_status 两边都产出（见 §2.4）
```

对应两个方法（`Detector.detect_info()` / `Detector.detect_usage()`），
`detect(usage=True)` 只是把两者组合起来。

**为什么必须拆？** spec 里给了理由：

> a caller that only needs inventory can obtain it without paying for utilization,
> temperature or power queries — **which on NVIDIA alone cost a 100 ms GPM sampling
> window per card**.

NVIDIA 的 SM 利用率要用 GPM（GPU Performance Monitoring）采样，**每张卡采两次样、
中间 sleep 100ms**（`_get_sm_util_from_gpm_metrics`，还带全局锁串行）。8 卡机器
就是 800ms。构建 CDI 配置、做拓扑计算、生成容器参数——这些场景一个用量字段都不需要，
却在拆分之前每次都要付这 800ms。

对 uMaaS 的直接映射：**Agent 上报的"节点硬件清单"和"GPU 实时指标"应当是两条独立的
采集路径，频率不同、失败语义不同。** 前者进数据库（慢变），后者进 Prometheus
（`ARCHITECTURE.md` §4.4 已经这么定了，这里补上"采集侧也要拆"）。

### 1.2 `index` 是枚举序号，不是设备号

这是本模块**改过一次的设计**，值得完整引用：

```python
index: int = 0
"""
Index of the device, as the detector enumerates it.
Driver-physical numbering, which a device node path or a vendor tool needs,
lives in `appendix` instead, e.g. `minor_number` or `card_id`/`physical_id`.
"""
```

原来的实现有一个开关 `GPUSTACK_RUNTIME_DETECT_PHYSICAL_INDEX_PRIORITY`，
默认让 `index` 取驱动的 minor number。这个开关被**删掉**了，原因见
[gpustack#6041](https://github.com/gpustack/gpustack/issues/6041)：
minor number 与 `CUDA_DEVICE_ORDER=PCI_BUS_ID` 的编号不一致，于是
"容器里的 GPU 2"和"记账里的 GPU 2"是两张不同的卡。

现在的规则：

| 用途 | 取哪个 |
|---|---|
| 调度、记账、`CUDA_VISIBLE_DEVICES` | `Device.index`（枚举序号） |
| 拼 `/dev/davinci{N}`、`/dev/dri/card{N}` 这类设备节点路径 | `appendix["minor_number"]` / `["card_id"]` / `["renderd_id"]` |

**两者混用的后果是给容器挂了另一张卡的设备节点**，而且症状极其隐蔽——
容器能起来，能看到一张卡，只是不是你以为的那张。

### 1.3 跳过一张卡，不能给其他卡重新编号

spec F4 里最有价值的一条决策：

> **A skipped device does not renumber its survivors.** The operator compacts its
> `Index`, so its second card becomes `Index 0` when the first is skipped. The runtime
> keeps the driver's enumeration index, because compacting would make the index
> *unstable across passes*: a transiently faulty card would shift every later card's
> index down and back again on recovery … A non-contiguous index is a static fact a
> caller can hold; a shifting one silently repoints a cached index at a different card.

翻译成运维语言：**卡 0 临时故障被跳过 → 卡 1 变成"0"号 → 数据库里记着
"部署 X 占用 0 号卡" → 卡 0 恢复后编号又变回去 → 记账指向了错误的卡。**

所以：**索引可以不连续、可以不从 0 开始，但绝不能因为别的卡的状态而移动。**
（昇腾就是这种情况：它上报的是 DCMI logic id，天然不连续。）

### 1.4 `appendix`：厂商特有信息的逃生舱

统一模型必然覆盖不了所有厂商，`appendix: dict[str, Any]` 是那个出口。
NVIDIA 往里放：

```python
{
  "arch_family": "hopper",          # 由 compute capability 推导
  "mig": True/False,                # 是否开了 MIG
  "bdf": "0000:1a:00.0",            # PCI 地址
  "minor_number": 3,                # 设备节点号
  "numa": "0-1",                    # NUMA 亲和
  "mig_devices": [...],             # MIG 实例列表
  "fabric_cluster_uuid": "...",     # NVSwitch fabric
  "fabric_clique_id": 2,
}
```

**关键：`appendix` 是有约定的，不是垃圾桶。** 每个 key 都有明确的消费者
（CDI 生成器读 `card_id`，拓扑读 `numa` 和 `bdf`，部署器读 `sliced`）。
uMaaS 的 `gpus` 表里也应该留一个 `attributes jsonb`，但同样要写清楚
"谁会读它"。

---

## 2. 检测：九家厂商的公因子

### 2.1 两级支持性探测：先扫 sysfs，再加载驱动库

```python
# detector/nvidia.py:47
@lru_cache(maxsize=1)
def is_supported() -> bool:
    if envs.GPUSTACK_RUNTIME_DETECT.lower() not in ("auto", "nvidia"):
        return False                       # ① 显式禁用
    pci_devs = NVIDIADetector.detect_pci_devices()   # ② 扫 sysfs 找 vendor=0x10de
    if not pci_devs and not envs.GPUSTACK_RUNTIME_DETECT_NO_PCI_CHECK:
        return False
    try:
        pynvml.nvmlInit()                  # ③ 才真正初始化驱动库
        return True
    except Exception:
        return False
```

第 ② 步是纯 sysfs 文件读（`/sys/bus/pci/devices/*/vendor`），**几毫秒，不加载
任何厂商库**。没有这一层，一台只有 NVIDIA 卡的机器每次检测都要尝试
`dlopen` 昇腾、寒武纪、海光…八个不存在的库，每次都产生一堆错误日志。

`GPUSTACK_RUNTIME_DETECT_NO_PCI_CHECK` 是给 WSL 用的逃生口——WSL 里
GPU 不出现在 PCI 总线上。

**给 uMaaS 的建议**：即使只支持 NVIDIA，也应当保留这个两级结构。
它同时解决了"节点没有 GPU 时 Agent 应该正常启动而不是崩溃"这个问题。

### 2.2 用量合并：按 UUID，永远不按 index

```python
# detector/__types__.py:518 merge_devices_usage()
"""
The usage query returns devices of its own, keyed by UUID, which this joins into
the devices to refresh … a metrics list is joined by device identity and
**never by index, as an index is not stable across a re-detection**.
"""
```

而且处理了一个真实的坏驱动场景：

```python
# 同一个 UUID 被多张卡上报 → 直接丢弃，不猜
usages_map, ambiguous_uuids = {}, set()
for usage in usages:
    if usage.uuid in usages_map:
        ambiguous_uuids.add(usage.uuid); continue
    usages_map[usage.uuid] = usage
for uuid in ambiguous_uuids:
    del usages_map[uuid]      # 取任何一条都会把 A 卡的温度写到 B 卡上
```

注释：*"taking either entry would write one card's utilization, memory and
temperature onto another. An ambiguous id is therefore dropped rather than
guessed at."*

**宁可没有指标，不可有错的指标。** 这条在 uMaaS 的 GPU 监控页上同样成立——
一个显示 N/A 的格子比一个显示错误温度的格子安全得多。

### 2.3 用量查询失败，不能连带丢掉清单

```python
# detector/__types__.py:676 Detector.detect()
devices = self.detect_info()
if usage and devices:
    try:
        self.detect_usage(devices)
    except Exception:
        # 清单已经拿到了，一张卡不管指标读没读到它都存在。
        # 让异常往上抛的代价是这个厂商的所有设备都没了 ——
        # 一台八卡健康的机器会因为一次指标查询失败而上报零张卡。
        debug_log_exception(...)
return devices
```

这是**分级降级**的教科书写法：清单是硬事实，用量是软信息，后者失败不该
污染前者。uMaaS 的 Agent 上报同理——**GPU 指标采集失败时，节点状态应当仍然
是 READY 且带完整拓扑**，只是指标缺失。反之（因为读不到温度就把节点判 NOT_READY）
会引发整机的模型实例迁移，代价大得多。

### 2.4 健康检查：默认关闭，因为它会挂死

`envs.py` 里 `GPUSTACK_RUNTIME_DETECT_NO_HEALTH_CHECK` **默认为 true**（即默认不做健康检查），
注释解释了原因：

> Each query is a driver call per device, and **against a wedged device it can block
> until the driver's RPC timeout (up to 45s with GSP firmware)**.

**这是一个必须知道的事实：读一张已经挂掉的 GPU 的 ECC 计数器，可能阻塞 45 秒。**
如果 Agent 的状态采集是同步的、没有超时，一张坏卡就能让整个节点的心跳停掉，
然后节点被判 NOT_READY，然后上面所有健康的实例被迁走——**一张坏卡导致整机失守**。

开启时的判定逻辑（`_get_memory_status`，对应最近一次提交 "fail closed on GPU
health check errors"）：

```python
try:
    ecc = nvmlDeviceGetMemoryErrorCounter(dev, UNCORRECTED, VOLATILE, DRAM)
    if ecc > 0: return UNHEALTHY
except NVMLError as e:
    # 失败即判死：驱动报错（GSP 卡死后 RPC 超时返回 ERROR_UNKNOWN）→ UNHEALTHY
    # 但 NOT_SUPPORTED 是"这张卡无法判断"，不是"这张卡坏了"
    if e.value != NVML_ERROR_NOT_SUPPORTED: return UNHEALTHY

# 只看 ECC 不够：GSP 故障（Xid 119/154）时 ECC 计数器可读且为 0
fields = nvmlDeviceGetFieldValues(dev, [GPU_RECOVERY_ACTION, RESET_STATUS])
for f in fields:
    if _extract_field_value(f): return UNHEALTHY
```

两条可直接借鉴：

1. **ECC 计数器为 0 不等于卡是好的。** GSP 固件故障（Xid 119/154）会让计数器
   保持可读的 0，必须额外查"是否需要 reset"的字段。
2. **区分"查询失败"与"不支持"。** 前者判死（fail closed），后者判不了
   （保持原判定）。把两者都当成"不支持"，坏卡就会一直被当好卡调度。

### 2.5 显存要报驱动报的数，不要"修正"

spec 里有一条被评审推翻的实现，理由极好：

> **NVIDIA GDDR ECC capacity restore — Withdrawn during PR review.**
> The restore was implemented, then removed: the operator's corrected figure is a
> *display* value that takes no part in allocation, while `memory` here does, and
> `memory - memory_used` has to mean free space. **Adding back capacity that is not
> reachable would over-commit every GDDR card with ECC on.**

GDDR 卡开 ECC 会吃掉约 1/16 的容量。参考实现（K8s operator）会把这部分加回去
用于显示；但这个包不加，因为它的 `memory` 字段**参与分配决策**。

**通则：同一个数字，用于展示和用于决策时口径可以不同，但必须分成两个字段。**
uMaaS 的 `gpus.memory_total_mb` 是记账用的，必须是"可分配量"；
若界面要显示标称容量，另开一个字段。

### 2.6 MIG：卡是稳定的库存单位，实例是易变的

```python
# 检测阶段：上报物理卡，MIG 实例塞进 appendix["mig_devices"]
# 消费阶段：需要寻址实例的调用方用 expand_mig_devices() 展开
```

理由（`expand_mig_devices` 的 docstring）：

> where a partitioner owns MIG, **the instances come and go and the card is the only
> stable inventory item**. … A MIG-enabled card with no instances is kept as-is:
> there is nothing to address yet, and dropping it would make the card disappear
> from the inventory.

MIG 实例的编号是**合成的**（`index_mig_devices`）：

```
每张卡分配一个大小为 slots（该卡能承载的最大 MIG 实例数）的编号块，
块起始位置 = 所有物理卡里最大 index + 1 + 卡序号 × slots
```

关键理由：**按"能承载多少"而不是"当前有多少"来划块，
使得给一张卡重新分区不会导致另一张卡的 MIG 实例被重新编号。**

uMaaS 首版不上 MIG，但这个"分块分配 ID 以隔离编号影响"的手法在别处也用得上。

### 2.7 拓扑：距离矩阵 + 近远排序

`Topology` 有三部分：距离矩阵、每卡 CPU 亲和、每卡 NUMA 亲和。距离用枚举分级：

```python
class TopologyDistanceEnum(int, Enum):
    SELF = 0     # 自己
    LINK = 5     # 高速互联：NVLink / AMD XGMI / 昇腾 HCCS
    PIX  = 10    # 经过至多一个 PCIe 桥
    PXB  = 20    # 经过多个 PCIe 桥（不过 Host Bridge）
    PHB  = 30    # 经过 PCIe 及 Host Bridge（通常是 CPU）
    NODE = 40    # 经过 PCIe 及 NUMA 节点间互联
    SYS  = 50    # 经过 QPI/UPI 跨 NUMA
    UNK  = 100
```

NVIDIA 的实现有一条捷径：

```python
# 如果某张卡有活跃的 NVLink，则认为同机所有卡都通过 NVLink 互联
if dev_i_links_state.get("links_active_count", 0) > 0:
    for j, dev_j in enumerate(devices):
        ret.devices_distances[i][j] = TopologyDistanceEnum.LINK
```

以及一个即用型的辅助函数：

```python
# detector/__types__.py:432
reduce_devices_distances(matrix) -> {0: [1, 2, 3], 1: [0, 2, 3], 2: [3, 1, 0], ...}
# 每张卡的"从近到远"的其他卡列表
```

**这正是 `SELF-HOSTED-GPU-OPERATIONS.md` §11.3 里"连续空闲 GPU"那个条件
应该有的实现。** "连续"是个错误的表述——真正要的是"互联距离近"，
而 4 张 index 连续的卡完全可能分属两个 PCIe root complex。
正确做法是：从每张空闲卡出发，按 `reduce_devices_distances` 的近远序
贪心取够 TP 需要的张数，再比较各起点的总距离，取最小。

### 2.8 NUMA 亲和：两条获取路径

```python
dev_numa = get_numa_node_by_bdf(dev_bdf)          # ① sysfs: /sys/bus/pci/devices/<bdf>/numa_node
if not dev_numa:                                   # ② NVML: 内存亲和位图
    dev_node_affinity = pynvml.nvmlDeviceGetMemoryAffinity(
        dev, get_numa_nodeset_size(), NVML_AFFINITY_SCOPE_NODE)
    dev_numa = bitmask_to_str(list(dev_node_affinity))
```

拿到 NUMA 节点后再映射到 CPU 集合（`map_numa_node_to_cpu_affinity`），
最终用于容器的 `cpuset_cpus` / `cpuset_mems`（§3.6）。

---

## 3. 部署：把"要几张卡"翻译成容器参数

`deployer/` 提供一套 **K8s 形状的 API**（`WorkloadPlan` / `Container` /
`ContainerResources`），后端可以是 Docker、Podman 或 K8s。上层代码（`gpustack.worker`）
写一次，三种底座都能跑。

### 3.1 `DevicesMaterial`：设备寻址的中枢

`deployer/__types__.py:1443`。这个 dataclass 是整个部署侧的核心，
它回答"一个设备在不同上下文里各叫什么名字"：

```python
DevicesMaterial(
    manufacturer = NVIDIA,
    runtime_env  = "NVIDIA_VISIBLE_DEVICES",     # ① 容器运行时（toolkit）读的
    backend_env  = ["CUDA_VISIBLE_DEVICES"],     # ② 应用（PyTorch/vLLM）读的
    cdi          = "nvidia.com/gpu",             # ③ CDI 资源名
    runtime_values  = {"0": "GPU-1111-2222-..."},          # ① 的值：优先用 UUID
    backend_values  = {"CUDA_VISIBLE_DEVICES": {"0": "0"}}, # ② 的值：索引
    numa_affinities = {"0": "0-1"},
    cpus_affinities = {"0": "0-7"},
)
```

**必须理解 ① 与 ② 是两层不同的东西**：

| | 运行时可见设备 | 后端可见设备 |
|---|---|---|
| 变量 | `NVIDIA_VISIBLE_DEVICES` | `CUDA_VISIBLE_DEVICES` |
| 谁消费 | nvidia-container-toolkit（在容器启动前挂设备节点） | CUDA 运行时（在进程内过滤） |
| 作用 | **决定容器里有哪些设备节点** | 决定应用看到哪几张 |
| 失效场景 | **容器 privileged 时失效**（见 §3.3） | 容器里被应用覆盖时失效 |

九家厂商的映射表（`envs.py` 默认值）：

```
amd.com/devices        = AMD_VISIBLE_DEVICES      → HIP_VISIBLE_DEVICES
huawei.com/devices     = ASCEND_VISIBLE_DEVICES   → ASCEND_RT_VISIBLE_DEVICES, NPU_VISIBLE_DEVICES
cambricon.com/devices  = CAMBRICON_VISIBLE_DEVICES→ MLU_VISIBLE_DEVICES
hygon.com/devices      = HYGON_VISIBLE_DEVICES    → HIP_VISIBLE_DEVICES
iluvatar.ai/devices    = IX_VISIBLE_DEVICES       → CUDA_VISIBLE_DEVICES
metax-tech.com/devices = CUDA_VISIBLE_DEVICES     → CUDA_VISIBLE_DEVICES
mthreads.com/devices   = METHERDS_VISIBLE_DEVICES → CUDA_VISIBLE_DEVICES, MUSA_VISIBLE_DEVICES
nvidia.com/devices     = NVIDIA_VISIBLE_DEVICES   → CUDA_VISIBLE_DEVICES
```

注意昇腾映射到**两个**后端变量，摩尔线程也是——一个变量装不下。

### 3.2 ★ `CUDA_DEVICE_ORDER=PCI_BUS_ID`：必须钉死

`map_visible_devices_ordering()` 的 docstring 是整个包里最该抄进 uMaaS 文档的一段：

> Only meaningful for a container seeing more than one device: it must number the
> devices as the detector, the driver and the vendor tooling do, otherwise **an index
> computed from detection addresses another device inside the container**.
> For example, CUDA's default ordering, `CUDA_DEVICE_ORDER=FASTEST_FIRST`, sorts the
> visible devices by a performance heuristic, **which reshuffles the ordinals on a
> heterogeneous host**, while `PCI_BUS_ID` sorts by PCI bus id, as NVML and the
> detector enumerate.

**CUDA 默认按"性能启发式"排序设备**，而 `nvidia-smi` 和 NVML 按 PCI 总线序。
在异构机器（比如一台机器上既有 A100 又有 T4，或不同代的卡）上，两者的编号不同。
后果：调度器记账说"用 0、1 号卡"，容器里的 `torch.cuda.device(0)` 指向另一张。

三条注入规则（很讲究）：

1. **只在容器能看到多于一张卡时注入。** 单卡容器没有排序可言。
2. **`privileged` 或请求 `all` 时一定注入**——这两种情况容器能看到全部卡。
   `all` 要**实际测量**展开成几张，而不是当成一个字面 token 计数
   （`count_requested_devices`）。
3. **是默认值，不是覆盖。** 容器自己声明了 `CUDA_DEVICE_ORDER` 就用它的：
   ```python
   create_options["environment"] = {**o_vs, **create_options["environment"]}
   ```

> **对 uMaaS 的直接影响**：`SELF-HOSTED-GPU-OPERATIONS.md` §11.4 的
> `docker run` 示例里没有这个变量。只要机器上的卡不完全同型号，
> 或者将来加了新卡，GPU 记账就会静默指向错误的设备。**这一条要加进模板。**

### 3.3 ★ `privileged` 会击穿设备隔离

`_create_containers` 里这一段极其重要：

```python
privileged = create_options.get("privileged", False)
...
if privileged:
    r_vs = self.get_runtime_visible_devices(ren, fmt)   # 全部设备，忽略请求
else:
    r_vs = self.map_runtime_visible_devices(ren, resource_values, fmt)
...
# 若不是请求全部但又是 privileged，必须用后端变量来限制
if r_v != "all" and privileged:
    b_vs = self.map_backend_visible_devices(runtime_envs, resource_values)
    create_options["environment"].update(b_vs)          # 写 CUDA_VISIBLE_DEVICES
```

**逻辑链条**：`privileged` 容器绕过 container toolkit 的设备挂载控制，
`/dev` 下所有设备节点都可见 → `NVIDIA_VISIBLE_DEVICES` 形同虚设 →
必须再用 `CUDA_VISIBLE_DEVICES`（应用层过滤）来限制。

而 GPUStack 部署 vLLM 时**恰恰是 `privileged=True` 的**（见
`GPUSTACK-DEEP-DIVE.md` §3.3）。也就是说：

> **在 privileged 模式下，GPU 隔离只剩下应用层这一道，靠 `CUDA_VISIBLE_DEVICES` 维持。
> 容器里的进程只要 `unset CUDA_VISIBLE_DEVICES`，就能用到全机所有卡。**

这对 uMaaS 是一个**安全与记账的双重问题**：

- 记账层面：一个行为异常的推理进程能占用未分配给它的卡，然后 §11.3 的三方
  对账会报出"实际占用 ≠ 记录"，而排查方向会跑偏。
- 安全层面：多租户下这等于没有隔离。

**结论：uMaaS 不应当默认 privileged。** 应当收敛到
`--device /dev/nvidiactl --device /dev/nvidia-uvm --device /dev/nvidia{i}` +
必要的 capability，让设备挂载本身成为隔离边界。若确实需要 privileged
（某些厂商驱动要求），必须同时设置后端变量，并在文档里写明"此时隔离是软的"。

### 3.4 三种设备注入方式

`GPUSTACK_RUNTIME_DOCKER_RESOURCE_INJECTION_POLICY` 可选：

| 方式 | 机制 | 适用 |
|---|---|---|
| `env`（默认） | 设 `NVIDIA_VISIBLE_DEVICES`，由 container toolkit 拦截 | 装了官方 toolkit 的 NVIDIA/AMD |
| `cdi` | `DeviceRequest(driver="cdi", device_ids=["nvidia.com/gpu=GPU-uuid"])` | 厂商中立，K8s/Podman 首选 |
| `kdp` | K8s device plugin 资源请求（`nvidia.com/gpu: "4"`） | K8s 上有 device plugin 时 |

**CDI（Container Device Interface）是趋势**，它把"这个设备需要挂哪些节点、哪些库、
跑哪些 hook"写成一份声明式 spec，运行时按 spec 执行，不需要厂商专属的 runtime hook。

`deployer/cdi/` 为**没有官方 toolkit 的厂商自己生成 CDI spec**。以 AMD 为例
（`cdi/amd.py`）：

```python
公共设备节点： /dev/kfd
每卡设备节点： /dev/dri/card{card_id}, /dev/dri/renderD{renderd_id}
                                  ↑ 注意读的是 appendix，不是 Device.index（§1.2）
每个设备注册两个名字： str(dev.index) 和 dev.uuid   ← 索引与 UUID 都能寻址
```

uMaaS 首版只上 NVIDIA，可以直接用官方 toolkit 的 env 方式。但**知道 CDI 存在
很重要**：它是"将来要支持国产卡"时唯一不需要重写部署层的路径。

### 3.5 `all` 要测量，不能当字面量

```python
def count_requested_devices(self, runtime_envs, resource_values) -> int:
    """"all" is a stand-in for every device the host has, so it is **measured**
    rather than counted as the single literal token it is written as."""
    if resource_values == ["all"]:
        return sum(len(self.get_runtime_visible_devices(re, "plain")) for re in runtime_envs)
    return len(resource_values)
```

小细节，但它决定了 §3.2 的"是否需要钉死排序"判断对不对：把 `all` 当成 1 个设备，
单卡逻辑就会错误地跳过排序注入。

### 3.6 CPU / NUMA 亲和

```python
map_visible_devices_affinities() -> {"cpuset_cpus": "0-7", "cpuset_mems": "0"}
```

由 `GPUSTACK_RUNTIME_DEPLOY_CPU_AFFINITY` / `..._NUMA_AFFINITY` 开关控制，
**默认关闭**。开启后，容器的 CPU 被限制在该 GPU 所属 NUMA 节点的核上。

收益：避免 host-to-device 拷贝跨 NUMA（PCIe 流量要走 QPI/UPI），
对 prefill 阶段的吞吐有可测量的影响。
代价：CPU 资源利用率下降，且多卡跨 NUMA 时这个约束会互相冲突
（代码里的处理是取并集）。

**建议 uMaaS 首版同样默认关闭，但把字段留好**——这是一个"压测能测出来才该开"的优化。

---

## 4. 部署器：在 Docker 上模拟 Pod 语义

### 4.1 Pause 容器

`_create_pause_container`。为了让一个 "workload" 能包含多个容器（主容器 + sidecar +
init 容器）并共享网络/IPC 命名空间，Docker 后端借用了 K8s 的 pause 容器手法：

```python
pause 容器：
  network_mode = "host" 或 "bridge"（端口映射挂在它身上）
  ipc_mode     = "shareable"（或 "host"）
  oom_score_adj = -998         # 最不容易被 OOM killer 选中
  privileged   = 任一 RUN 容器是否 privileged
  security_opt = ["no-new-privileges"]

其余容器：
  network_mode = f"container:{pause.id}"
  ipc_mode     = f"container:{pause.id}"
```

好处：主容器崩溃重建时，**命名空间和端口映射不变**——IP 不变、端口不变，
上游路由不需要更新。这正是 K8s 里 pause 容器存在的理由。

对 uMaaS 的意义：如果将来要给推理实例配 sidecar（比如指标转换、日志采集、
KV Cache 服务），这是在裸 Docker 上的标准解法。**首版单容器不需要**，
但要知道扩展路径是什么，别把"一个部署 = 一个容器"写死进数据模型。

### 4.2 Docker 没有 liveness probe → autoheal sidecar

Docker 的 `HEALTHCHECK` 只会把容器标成 unhealthy，**不会重启它**。
所以当某个容器的检查配了 `teardown=True` 时，额外起一个 sidecar：

```python
container_name = f"{workload.name}-unhealthy-restart"
restart_policy = "always"
network_mode   = "none"
volumes        = ["/var/run/docker.sock:/var/run/docker.sock"]
environment    = ["AUTOHEAL_CONTAINER_LABEL=<heal-label>-<workload>"]
# 被监控的容器打上这个 label
```

**uMaaS 不需要这个**——§11.4 已定"容器 `--restart no`，重启由调谐循环执行"，
控制面本来就在做这件事，再加一个 sidecar 反而多一个动作源（违反 §11.5.3
"只留一条拆除路径"）。这里记下来是作为**对照**：GPUStack 之所以需要它，
是因为它的 workload 抽象要在没有控制面参与的情况下自洽。

### 4.3 工作负载状态：重启策略改变判定

`DockerWorkloadStatus.parse_state()` 里有一条很容易忽略的逻辑：

```python
if cr.status == "exited":
    if cr.attrs["State"].get("ExitCode", 1) != 0:
        return FAILED if not _has_restart_policy(cr) else UNHEALTHY
    return INACTIVE
```

**同样是"非零退出"，有重启策略时判 `UNHEALTHY`（还会自己起来），
没有重启策略时判 `FAILED`（终态）。** 状态机的语义依赖于容器的配置，
不能只看容器的 status 字符串。

状态机本身：

```
UNKNOWN → PENDING → INITIALIZING → RUNNING → FAILED | UNHEALTHY | INACTIVE
```

`INITIALIZING` 由 **init 容器**是否跑完决定——这也是 K8s 语义的移植。
uMaaS §11.4 的 `loading` 状态在语义上对应这里的 `INITIALIZING`，
但 GPUStack 的实现更细：它靠"init 容器是否退出"来判断，
而不是靠"HTTP 探针失败了多久"来猜。

**如果 uMaaS 用 init 容器做权重预热/校验，`loading` 阶段就变成一个确定的事实
而不是一个宽限期猜测。** 这比现在文档里的"启动宽限期"方案更可靠——
宽限期设长了故障发现慢，设短了正常启动被误杀。

### 4.4 结构化退出信息

`WorkloadStatusExit`：

```python
name, token, exit_code, reason, message, started_at, finished_at, restart_count
```

`reason` 举例：`OOMKilled` / `Error` / `ImagePullBackOff` / `ErrImageNeverPull`。

两个细节：

```python
_CONTAINER_EXITED_STATUSES = ("exited", "dead", "restarting")
"""
"restarting" is one of them: a crash-looping container sits there between attempts
with the last attempt's ExitCode already filled in, and that code is the diagnosis
being asked for.
"""

_CONTAINER_UNSET_TIMESTAMP_PREFIX = "0001-01-01"
"""
What Docker fills an unset timestamp with -- Go's zero time -- rather than
omitting the key, which is why a plain `.get(..., "")` never defaults.
"""
```

第一条：**崩溃循环的容器处于 `restarting` 状态，而退出码已经填好了**——
这正是要诊断的那个码。只查 `exited` 会漏掉最需要诊断的情况。

第二条：Docker 的未设置时间戳是 Go 零值 `0001-01-01T00:00:00Z` 而不是缺 key，
`.get(k, "")` 拿不到默认值。这类"上游 API 的空值不是你以为的空值"的坑，
Go 写的 uMaaS 在解析 Docker API 时同样会遇到。

K8s 侧还会去读 Pod Events 来解释镜像拉取失败，并且：

> The appended Event is selected by `involvedObject.uid` as well as name, chosen by
> **timestamp rather than list order — the API guarantees none** — and prefers the
> kubelet's `Failed` Event, so a stale `FailedScheduling` cannot stand in as the
> diagnosis.

**列表 API 不保证顺序**，按时间戳选而不是取第一个。

---

## 5. 工程实践：几个值得抄的模式

### 5.1 `envs.py`：单一模块声明全部环境变量

37KB 的一个文件，结构是：

```python
if TYPE_CHECKING:
    GPUSTACK_RUNTIME_DETECT_NO_HEALTH_CHECK: bool = True
    """
    Set true to disable the health check during detection …
    （长注释解释这个开关意味着什么、为什么是这个默认值、代价是什么）
    """

_LOADERS = {
    "GPUSTACK_RUNTIME_DETECT_NO_HEALTH_CHECK": lambda: to_bool(getenv(..., "1")),
}
```

三个好处：
1. **所有可调项在一个地方**，可以直接生成文档（`docs/environment-variables.md`）。
2. **类型声明与加载逻辑分离**，IDE 有补全，运行时懒加载。
3. **每个变量都有解释代价的注释**，而不只是"设 true 开启 X"。

uMaaS 用 koanf（`ARCHITECTURE.md` §2.5），结构不同，但"每个配置项的注释要写清楚
代价与默认值理由"这条同样成立。

### 5.2 手写 ctypes 绑定，而不是 shell out

寒武纪的检测原来是 `cnmon info -e -m -u -j` 然后解析 JSON，spec 里把它重写成了
手写的 `pycndev` ctypes 绑定。理由（从 spec 的对比表读出来）：shell out 版本
拿不到 driver version、Neuware version、cores、power、BDF、NUMA、health/ECC、PCIe bus id
——**一个都拿不到**，因为 CLI 只吐它想吐的。

**通则：和硬件打交道优先用库，shell out 是最后手段。** `nvidia-smi` 的输出格式
在版本间会变，且拿不到 NVML 里一半的字段。uMaaS 的 Agent 应当用
`go-nvml`（NVIDIA 官方 Go 绑定）而不是 `exec.Command("nvidia-smi")`。

### 5.3 `debug_log_exception`：调试期才打堆栈

`logging.py` 里定义了 `debug_log_exception` / `debug_log_warning`，
只在 `LOG_LEVEL=DEBUG` 时才输出完整异常，正常运行只留一行。
硬件检测这类"预期会有大量失败"的代码路径（八个厂商里七个必然失败）
必须这么处理，否则日志会被淹没。

### 5.4 每个分歧都在代码旁边写清楚原因

这是本仓库最突出的工程习惯。随手一例：

```python
# Reported as the driver reports it, i.e. what the card can actually allocate.
# A deliberate divergence: the operator adds back the ~1/16 that ECC parity carves
# out of a GDDR part, but that capacity is not reachable, and its restored figure is
# a display value that takes no part in allocation. Here `memory` does take part, and
# `memory - memory_used` has to mean free space, so restoring it would over-commit
# every GDDR card with ECC enabled.
dev_mem, _ = _get_memory_info(dev)
```

spec 里甚至有一条验收标准是：*"Every gap above is either fixed or annotated as
deliberate; **no gap is left silently open**."*

**对 uMaaS 的建议**：GPU 检测/记账这一层的每个 magic number、每个 vendor 特判、
每个"看起来可以简化但不能"的分支，都要在代码旁写清原因。这一层的代码
半年后没人敢碰，注释是唯一的资产。

---

## 6. 直接可借鉴清单

按对 `SELF-HOSTED-GPU-OPERATIONS.md` 的影响排序。

### 6.1 检测侧

| # | 结论 | 位置 |
|---|---|---|
| 1 | **清单查询与用量查询必须拆开**，NVIDIA 的 SM 利用率每卡要 100ms GPM 采样窗 | `detector/__types__.py:649` |
| 2 | **用量按 UUID 合并，永不按 index**——index 跨检测不稳定 | `__types__.py:518` |
| 3 | **同一 UUID 被多卡上报时丢弃，不猜**——猜会把 A 卡指标写到 B 卡 | `__types__.py:553` |
| 4 | **用量查询失败不能丢掉清单**，否则一次指标失败让八卡机器上报零卡 | `__types__.py:690` |
| 5 | **跳过故障卡不能给存活卡重新编号**，否则缓存的索引指向另一张卡 | spec F4 |
| 6 | **`index`（枚举序）与设备节点号（minor/card_id）是两个东西**，混用会挂错设备节点 | `__types__.py:163` |
| 7 | **健康检查会在坏卡上阻塞至 45 秒**（GSP RPC 超时）——采集必须有超时 | `envs.py:47` |
| 8 | **ECC 计数为 0 不代表卡好**，GSP 故障（Xid 119/154）需另查 recovery/reset 字段 | `nvidia.py:573` |
| 9 | **健康查询失败判死（fail closed），"不支持"判不了**——两者必须区分 | `nvidia.py:566` |
| 10 | **显存报驱动报的数，不做 ECC 容量修正**——修正会让每张开 ECC 的 GDDR 卡超分 | `nvidia.py:192` |
| 11 | **支持性探测两级**：先扫 sysfs PCI vendor，再加载驱动库 | `nvidia.py:47` |
| 12 | **拓扑距离分级**（LINK/PIX/PXB/PHB/NODE/SYS）+ 每卡近远排序 | `__types__.py:362` |
| 13 | **NUMA 亲和两条路径**：sysfs `numa_node` 优先，NVML 内存亲和位图兜底 | `nvidia.py:146` |

### 6.2 部署侧

| # | 结论 | 位置 |
|---|---|---|
| 14 | **★ `CUDA_DEVICE_ORDER=PCI_BUS_ID` 必须钉死**，CUDA 默认按性能排序，异构机上编号与 NVML 不一致 | `__types__.py:1842` |
| 15 | **★ `privileged` 容器看得到全部卡**，运行时可见变量失效，必须再设后端可见变量 | `docker.py:1100` |
| 16 | **运行时可见 vs 后端可见是两层**（`NVIDIA_VISIBLE_DEVICES` vs `CUDA_VISIBLE_DEVICES`） | `__types__.py:1450` |
| 17 | **`NVIDIA_VISIBLE_DEVICES` 优先用 UUID 而不是索引** | `__types__.py:1607` |
| 18 | **`all` 要展开测量**，不能当成 1 个 token | `__types__.py:1813` |
| 19 | **排序变量是默认值不是覆盖**，容器自己声明了就用它的 | `docker.py:1148` |
| 20 | **`restarting` 状态的容器已经填好了上次的退出码**——诊断信息在这里 | `__types__.py:1221` |
| 21 | **Docker 未设置的时间戳是 Go 零值 `0001-01-01`，不是缺 key** | `__types__.py:1228` |
| 22 | **同样是非零退出，有无重启策略决定判 UNHEALTHY 还是 FAILED** | `docker.py:219` |
| 23 | **列表型 API 不保证顺序**，选事件按时间戳而不是取第一个 | spec F5 |
| 24 | **CPU/NUMA 亲和默认关**，是压测能测出收益才该开的优化 | `__types__.py:1898` |
| 25 | **init 容器让"还在加载"成为确定事实**，而不是靠宽限期猜 | `docker.py:238` |

### 6.3 工程习惯

| # | 结论 |
|---|---|
| 26 | 和硬件打交道用库绑定，不 shell out CLI——CLI 拿不到一半字段且格式会变 |
| 27 | 预期高频失败的路径（多厂商探测）用 debug 级异常日志，否则日志被淹没 |
| 28 | **每个与参考实现的分歧都要在代码旁注明原因**，"no gap is left silently open" |
| 29 | 环境变量集中声明，每项注释要写清代价与默认值理由 |
| 30 | 设计文档（spec）逐项记录"哪里不一样、为什么"，比代码本身更有长期价值 |

---

## 7. 不建议照抄

| 做法 | 理由 |
|---|---|
| 九厂商全支持 | uMaaS 已定首版只上 vLLM + NVIDIA。多厂商抽象的代价是每个字段都要找九家的最小公倍数 |
| Docker 上的 pause 容器 | 首版单容器部署不需要；引入它会让"一个部署 = 一个容器"的模型变复杂 |
| autoheal sidecar | 与 §11.5.3「只留一条拆除路径」冲突——控制面已经在做重启 |
| `privileged=True` 默认 | 见 §3.3，多租户下等于没有隔离 |
| 健康检查默认关闭 | 它默认关是因为它是个库、不知道调用方的超时预算。uMaaS 是自己的 Agent，应当**开启但带超时**（见 §6.1 第 7 条） |
| 手写 ctypes 绑定 | Go 侧有官方的 `go-nvml`，不需要自己写 |

---

## 附：复核入口

```bash
cd opensource/runtime

# 核心抽象与合并语义
sed -n '150,240p'   gpustack_runtime/detector/__types__.py   # Device 字段划分
sed -n '460,610p'   gpustack_runtime/detector/__types__.py   # MIG 编号、按 UUID 合并
sed -n '609,720p'   gpustack_runtime/detector/__types__.py   # Detector 基类与降级

# NVIDIA 检测
sed -n '45,100p'    gpustack_runtime/detector/nvidia.py      # 两级支持性探测
sed -n '160,270p'   gpustack_runtime/detector/nvidia.py      # 清单查询
sed -n '273,390p'   gpustack_runtime/detector/nvidia.py      # 用量查询
sed -n '526,595p'   gpustack_runtime/detector/nvidia.py      # 健康判定（fail closed）
sed -n '387,485p'   gpustack_runtime/detector/nvidia.py      # 拓扑

# 设备寻址与注入
sed -n '1443,1500p' gpustack_runtime/deployer/__types__.py   # DevicesMaterial
sed -n '1842,1930p' gpustack_runtime/deployer/__types__.py   # ★ 排序与亲和
sed -n '990,1170p'  gpustack_runtime/deployer/docker.py      # ★ 资源翻译与 privileged

# 容器编排
sed -n '166,310p'   gpustack_runtime/deployer/docker.py      # 状态解析
sed -n '547,720p'   gpustack_runtime/deployer/docker.py      # pause / autoheal

# 设计文档（最有价值）
less specs/2026-08-14-detector-alignment-and-workload-exit-status.md
```
