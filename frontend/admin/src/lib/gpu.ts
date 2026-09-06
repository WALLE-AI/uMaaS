/** GPU 领域知识。与数据来源无关，因此不放在 mock 里。 */
/**
 * XID 错误码释义表。
 *
 * DCGM / nvidia-smi 只吐一个数字，管理者不该为了看懂告警去翻 NVIDIA 文档。
 * 每条附带处置建议 —— 有些 XID 换驱动就行，有些必须换卡，二者代价差很远。
 */
export const XID_MEANING: Record<number, { title: string; detail: string; action: string }> = {
  13: {
    title: 'Graphics Engine Exception',
    detail: '通常由非法内存访问引起，多为应用侧问题而非硬件故障。',
    action: '检查推理框架版本与 CUDA 兼容性，一般无需换卡。',
  },
  31: {
    title: 'GPU Memory Page Fault',
    detail: '应用访问了非法显存地址，绝大多数是软件缺陷。',
    action: '排查推理框架或自定义算子，先不要动硬件。',
  },
  43: {
    title: 'GPU Stopped Processing',
    detail: '任务被 GPU 主动中止，通常伴随应用异常退出。',
    action: '查看实例日志确认是否 OOM 或超时。',
  },
  48: {
    title: 'Double Bit ECC Error',
    detail: '不可纠正的显存错误，数据已损坏。这是硬件故障。',
    action: '立即驱逐实例并隔离该卡，联系厂商 RMA 更换。',
  },
  63: {
    title: 'ECC Page Retirement',
    detail: '显存页已被退休或行重映射，属于自愈机制生效。',
    action: '记录并观察；退休页数持续增长意味着显存正在劣化。',
  },
  74: {
    title: 'NVLink Error',
    detail: 'NVLink 链路异常，会导致张量并行的通信降速或失败。',
    action: '检查物理连接与拓扑；影响多卡并行的实例。',
  },
  79: {
    title: 'GPU Has Fallen Off The Bus',
    detail: 'GPU 从 PCIe 总线掉线，主机已无法访问该设备。',
    action: '需重启节点；反复出现应更换硬件或排查供电散热。',
  },
}
