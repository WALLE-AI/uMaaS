const models = [
  {
    id: "deepseek-ai/DeepSeek-V3.2",
    vendor: "DeepSeek",
    logo: "DS",
    logoClass: "deepseek",
    type: "对话",
    capabilities: ["对话", "推理", "Tools", "MoE", "长文本"],
    specs: ["671B", "128K"],
    inputPrice: 2,
    outputPrice: 3,
    released: "2026-08-21",
    badge: "New",
    badgeType: "new",
    context: 128,
    params: "671B MoE",
    rpm: "1,000 RPM",
    tpm: "50,000 TPM",
    description: "面向复杂推理与 Agent 场景的新一代通用模型，在代码生成、工具调用和长上下文理解方面表现稳定。"
  },
  {
    id: "Pro/deepseek-ai/DeepSeek-V3.2",
    vendor: "DeepSeek",
    logo: "DS",
    logoClass: "deepseek",
    type: "对话",
    capabilities: ["对话", "推理", "Tools", "MoE", "长文本"],
    specs: ["671B", "128K"],
    inputPrice: 4,
    outputPrice: 6,
    released: "2026-08-21",
    badge: "New",
    badgeType: "new",
    context: 128,
    params: "671B MoE",
    rpm: "3,000 RPM",
    tpm: "200,000 TPM",
    description: "高优先级推理服务版本，提供更高并发和更稳定的首 Token 延迟，适合线上生产工作负载。"
  },
  {
    id: "deepseek-ai/DeepSeek-R1",
    vendor: "DeepSeek",
    logo: "DS",
    logoClass: "deepseek",
    type: "对话",
    capabilities: ["对话", "推理", "Tools", "Math"],
    specs: ["671B", "128K"],
    inputPrice: 4,
    outputPrice: 16,
    released: "2025-05-28",
    context: 128,
    params: "671B MoE",
    rpm: "1,000 RPM",
    tpm: "100,000 TPM",
    description: "通过强化学习训练的推理模型，擅长数学、代码与复杂问题求解，并支持结构化工具调用。"
  },
  {
    id: "moonshotai/Kimi-K2-Thinking",
    vendor: "Moonshot AI",
    logo: "K",
    logoClass: "moonshot",
    type: "对话",
    capabilities: ["对话", "推理", "Tools", "长文本"],
    specs: ["1T", "256K"],
    inputPrice: 4,
    outputPrice: 16,
    released: "2026-07-19",
    badge: "New",
    badgeType: "new",
    context: 256,
    params: "1T MoE",
    rpm: "800 RPM",
    tpm: "80,000 TPM",
    description: "为深度思考和长程任务构建，具备优秀的上下文保持能力与多步骤 Agent 执行能力。"
  },
  {
    id: "Qwen/Qwen3-VL-32B-Instruct",
    vendor: "Qwen",
    logo: "Q",
    logoClass: "qwen",
    type: "视觉",
    capabilities: ["视觉", "多模态", "Tools", "长文本"],
    specs: ["32B", "256K"],
    inputPrice: 4,
    outputPrice: 12,
    released: "2026-07-28",
    badge: "New",
    badgeType: "new",
    context: 256,
    params: "32B Dense",
    rpm: "1,000 RPM",
    tpm: "100,000 TPM",
    description: "视觉语言模型，支持文档、图表、界面和多图理解，兼顾文本生成与结构化输出。"
  },
  {
    id: "Qwen/Qwen3-VL-8B-Instruct",
    vendor: "Qwen",
    logo: "Q",
    logoClass: "qwen",
    type: "视觉",
    capabilities: ["视觉", "多模态", "Tools"],
    specs: ["8B", "256K"],
    inputPrice: 0,
    outputPrice: 0,
    released: "2026-06-12",
    badge: "免费",
    badgeType: "free",
    context: 256,
    params: "8B Dense",
    rpm: "100 RPM",
    tpm: "20,000 TPM",
    description: "轻量级视觉语言模型，适合 OCR、截图理解、基础视觉问答和低成本原型验证。"
  },
  {
    id: "MiniMaxAI/MiniMax-M2",
    vendor: "MiniMax",
    logo: "M",
    logoClass: "minimax",
    type: "对话",
    capabilities: ["对话", "推理", "Tools", "Coder"],
    specs: ["230B", "128K"],
    inputPrice: 1.2,
    outputPrice: 4.8,
    released: "2026-06-30",
    context: 128,
    params: "230B MoE",
    rpm: "1,200 RPM",
    tpm: "120,000 TPM",
    description: "面向高效 Agent 与代码任务优化，在较低推理成本下保持良好的工具使用和指令遵循能力。"
  },
  {
    id: "zai-org/GLM-4.6",
    vendor: "Zhipu AI",
    logo: "Z",
    logoClass: "zhipu",
    type: "对话",
    capabilities: ["对话", "推理", "Tools", "Coder"],
    specs: ["355B", "200K"],
    inputPrice: 2,
    outputPrice: 8,
    released: "2026-05-22",
    context: 200,
    params: "355B MoE",
    rpm: "1,000 RPM",
    tpm: "100,000 TPM",
    description: "通用旗舰模型，强化了代码、推理和智能体能力，支持长上下文与稳定工具调用。"
  },
  {
    id: "Qwen/Qwen3-30B-A3B-Thinking",
    vendor: "Qwen",
    logo: "Q",
    logoClass: "qwen",
    type: "对话",
    capabilities: ["对话", "推理", "Math", "Coder"],
    specs: ["30B-A3B", "128K"],
    inputPrice: 0.8,
    outputPrice: 3.2,
    released: "2026-04-14",
    context: 128,
    params: "30B-A3B MoE",
    rpm: "800 RPM",
    tpm: "80,000 TPM",
    description: "小型 MoE 推理模型，仅激活 3B 参数，适合对延迟和成本敏感的推理与编码任务。"
  },
  {
    id: "BAAI/bge-m3",
    vendor: "BAAI",
    logo: "B",
    logoClass: "bge",
    type: "嵌入",
    capabilities: ["嵌入", "多语言", "长文本"],
    specs: ["568M", "8K"],
    inputPrice: 0,
    outputPrice: 0,
    released: "2025-09-08",
    badge: "免费",
    badgeType: "free",
    context: 8,
    params: "568M Dense",
    rpm: "2,000 RPM",
    tpm: "500,000 TPM",
    description: "支持多语言、多粒度和多功能检索的通用向量模型，适合 RAG、语义搜索和文本聚类。"
  },
  {
    id: "Qwen/Qwen-Image-Edit",
    vendor: "Qwen",
    logo: "Q",
    logoClass: "qwen",
    type: "图像",
    capabilities: ["图像", "多模态", "图像编辑"],
    specs: ["20B", "2K"],
    inputPrice: 0.18,
    outputPrice: 0.18,
    priceUnit: "/ 张",
    released: "2026-03-09",
    context: 2,
    params: "20B Diffusion",
    rpm: "120 RPM",
    tpm: "按图像计费",
    description: "支持自然语言驱动的图像编辑、局部重绘、风格变换与多轮视觉修改。"
  },
  {
    id: "FunAudioLLM/CosyVoice2-0.5B",
    vendor: "FunAudioLLM",
    logo: "F",
    logoClass: "minimax",
    type: "语音",
    capabilities: ["语音", "多语言", "音色克隆"],
    specs: ["0.5B", "48kHz"],
    inputPrice: 1,
    outputPrice: 1,
    priceUnit: "/ 万字符",
    released: "2025-11-18",
    context: 1,
    params: "0.5B",
    rpm: "300 RPM",
    tpm: "按字符计费",
    description: "自然流畅的多语言语音合成模型，支持情感控制、跨语言生成和预置音色。"
  }
];

const state = {
  search: "",
  type: "全部",
  capability: "全部",
  vendor: "全部",
  freeOnly: false,
  sort: "recommended",
  favorites: new Set(JSON.parse(localStorage.getItem("sf-favorites") || "[]")),
  activeModel: null,
  lastFocused: null
};

const grid = document.querySelector("#modelGrid");
const count = document.querySelector("#resultCount");
const emptyState = document.querySelector("#emptyState");
const searchInput = document.querySelector("#searchInput");
const searchBox = searchInput.closest(".search-box");
const activeFilters = document.querySelector("#activeFilters");
const filterToggle = document.querySelector("#filterToggle");
const filterPanel = document.querySelector("#filterPanel");
const drawerLayer = document.querySelector("#drawerLayer");
const drawer = document.querySelector("#modelDrawer");
const drawerContent = document.querySelector("#drawerContent");

const iconRefresh = () => window.lucide?.createIcons({ attrs: { "aria-hidden": "true" } });
const formatPrice = (price) => price === 0 ? "免费" : `￥${Number.isInteger(price) ? price : price.toFixed(2)}`;

function getFilteredModels() {
  const query = state.search.trim().toLowerCase();
  const filtered = models.filter((model) => {
    const haystack = [model.id, model.vendor, model.type, ...model.capabilities, ...model.specs].join(" ").toLowerCase();
    return (!query || haystack.includes(query))
      && (state.type === "全部" || model.type === state.type || model.capabilities.includes(state.type))
      && (state.capability === "全部" || model.capabilities.includes(state.capability))
      && (state.vendor === "全部" || model.vendor === state.vendor)
      && (!state.freeOnly || (model.inputPrice === 0 && model.outputPrice === 0));
  });

  return filtered.sort((a, b) => {
    if (state.sort === "newest") return b.released.localeCompare(a.released);
    if (state.sort === "price-low") return (a.inputPrice + a.outputPrice) - (b.inputPrice + b.outputPrice);
    if (state.sort === "context-high") return b.context - a.context;
    return Number(Boolean(b.badge)) - Number(Boolean(a.badge));
  });
}

function renderModels() {
  const visible = getFilteredModels();
  count.textContent = visible.length;
  grid.hidden = visible.length === 0;
  emptyState.hidden = visible.length !== 0;
  grid.innerHTML = visible.map((model) => {
    const unit = model.priceUnit || "/ M Tokens";
    const price = model.inputPrice === model.outputPrice
      ? `${formatPrice(model.inputPrice)} ${model.inputPrice ? unit : ""}`
      : `${formatPrice(model.inputPrice)} / ${formatPrice(model.outputPrice)} ${unit}`;
    const isFavorite = state.favorites.has(model.id);
    return `
      <article class="model-card" tabindex="0" role="button" aria-label="查看 ${model.id} 详情" data-model-id="${model.id}">
        ${model.badge ? `<span class="status-badge ${model.badgeType}">${model.badge}</span>` : ""}
        <div class="model-head">
          <div class="model-logo ${model.logoClass}">${model.logo}</div>
          <div class="model-heading">
            <span class="model-name" title="${model.id}">${model.id}</span>
            <div class="model-meta"><span>${model.vendor}</span><span>·</span><strong>${price}</strong></div>
          </div>
        </div>
        <p class="model-description">${model.description}</p>
        <div class="model-tags">
          ${model.capabilities.slice(0, 5).map((item) => `<span class="tag">${item}</span>`).join("")}
          ${model.specs.map((item) => `<span class="tag spec">${item}</span>`).join("")}
        </div>
        <button class="favorite-button ${isFavorite ? "active" : ""}" type="button" aria-label="${isFavorite ? "取消收藏" : "收藏"} ${model.id}" aria-pressed="${isFavorite}" data-favorite="${model.id}">
          <i data-lucide="star"></i>
        </button>
      </article>`;
  }).join("");
  renderActiveFilters();
  iconRefresh();
}

function renderActiveFilters() {
  const items = [];
  if (state.type !== "全部") items.push(["type", state.type]);
  if (state.capability !== "全部") items.push(["capability", state.capability]);
  if (state.vendor !== "全部") items.push(["vendor", state.vendor]);
  if (state.freeOnly) items.push(["freeOnly", "免费"]);
  activeFilters.innerHTML = items.map(([key, value]) => `<button class="active-filter" type="button" data-remove-filter="${key}">${value}<i data-lucide="x"></i></button>`).join("");
}

function resetFilters() {
  state.search = "";
  state.type = "全部";
  state.capability = "全部";
  state.vendor = "全部";
  state.freeOnly = false;
  searchInput.value = "";
  searchBox.classList.remove("has-value");
  document.querySelectorAll(".filter-options").forEach((group) => {
    group.querySelectorAll(".filter-pill").forEach((button) => button.classList.toggle("active", button.dataset.value === "全部"));
  });
  const freeButton = document.querySelector("#freeFilter");
  freeButton.setAttribute("aria-pressed", "false");
  renderModels();
}

function openDrawer(model, trigger) {
  state.activeModel = model;
  state.lastFocused = trigger;
  const unit = model.priceUnit || "/ M Tokens";
  const code = `curl https://api.siliconflow.cn/v1/chat/completions \\\n+  -H "Authorization: Bearer $SILICONFLOW_API_KEY" \\\n+  -H "Content-Type: application/json" \\\n+  -d '{"model":"${model.id}","messages":[{"role":"user","content":"你好"}]}'`;
  drawerContent.innerHTML = `
    <div class="drawer-header">
      <span>模型详情</span>
      <button class="icon-button" type="button" id="drawerClose" aria-label="关闭详情"><i data-lucide="x"></i></button>
    </div>
    <div class="drawer-body">
      <div class="detail-title-row">
        <div class="model-logo ${model.logoClass}">${model.logo}</div>
        <div class="detail-title"><h2 id="drawerTitle">${model.id}</h2><div class="vendor-line">由 ${model.vendor} 提供</div></div>
        <button class="copy-id" type="button" data-copy="${model.id}" aria-label="复制模型 ID"><i data-lucide="copy"></i></button>
      </div>
      <p class="detail-description">${model.description}</p>
      <div class="detail-tags">${[...model.capabilities, ...model.specs].map((tag) => `<span class="tag">${tag}</span>`).join("")}</div>
      <section class="section-block"><h3 class="section-title">模型价格</h3><div class="price-grid">
        <div class="price-item"><span>输入价格</span><strong>${formatPrice(model.inputPrice)}</strong>${model.inputPrice ? `<small>${unit}</small>` : ""}</div>
        <div class="price-item"><span>输出价格</span><strong>${formatPrice(model.outputPrice)}</strong>${model.outputPrice ? `<small>${unit}</small>` : ""}</div>
      </div></section>
      <section class="section-block"><h3 class="section-title">服务规格</h3><div class="spec-table">
        <div class="spec-row"><span>模型厂商</span><strong>${model.vendor}</strong></div>
        <div class="spec-row"><span>参数规模</span><strong>${model.params}</strong></div>
        <div class="spec-row"><span>上下文长度</span><strong>${model.context}K Tokens</strong></div>
        <div class="spec-row"><span>请求限速</span><strong>${model.rpm}</strong></div>
        <div class="spec-row"><span>Token 限速</span><strong>${model.tpm}</strong></div>
        <div class="spec-row"><span>发布时间</span><strong>${model.released}</strong></div>
      </div></section>
      <section class="section-block"><h3 class="section-title">快速调用</h3><div class="code-block"><pre>${escapeHtml(code)}</pre><button class="code-copy" type="button" data-copy="${escapeHtml(code)}" aria-label="复制调用代码"><i data-lucide="copy"></i></button></div></section>
    </div>
    <div class="drawer-actions">
      <button class="secondary-action" type="button" data-toast="API 文档已在新窗口打开"><i data-lucide="book-open"></i>API 文档</button>
      <button class="primary-action" type="button" data-toast="已进入 ${model.id} 在线体验"><i data-lucide="play"></i>在线体验</button>
    </div>`;
  drawerLayer.classList.add("open");
  drawerLayer.setAttribute("aria-hidden", "false");
  document.body.classList.add("drawer-open");
  window.location.hash = `model=${encodeURIComponent(model.id)}`;
  iconRefresh();
  requestAnimationFrame(() => document.querySelector("#drawerClose")?.focus());
}

function closeDrawer({ updateHash = true } = {}) {
  drawerLayer.classList.remove("open");
  drawerLayer.setAttribute("aria-hidden", "true");
  document.body.classList.remove("drawer-open");
  state.activeModel = null;
  if (updateHash && window.location.hash.startsWith("#model=")) history.replaceState(null, "", window.location.pathname + window.location.search);
  state.lastFocused?.focus();
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[char]);
}

function showToast(message) {
  const toast = document.createElement("div");
  toast.className = "toast";
  toast.innerHTML = `<i data-lucide="circle-check"></i><span>${message}</span>`;
  document.querySelector("#toastRegion").appendChild(toast);
  iconRefresh();
  setTimeout(() => toast.remove(), 2800);
}

async function copyText(value) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const input = document.createElement("textarea");
    input.value = value;
    document.body.appendChild(input);
    input.select();
    document.execCommand("copy");
    input.remove();
  }
  showToast("已复制到剪贴板");
}

filterToggle.addEventListener("click", () => {
  const expanded = filterToggle.getAttribute("aria-expanded") === "true";
  filterToggle.setAttribute("aria-expanded", String(!expanded));
  filterToggle.querySelector("span").textContent = expanded ? "展开筛选器" : "收起筛选器";
  filterPanel.hidden = expanded;
});

searchInput.addEventListener("input", (event) => {
  state.search = event.target.value;
  searchBox.classList.toggle("has-value", Boolean(state.search));
  renderModels();
});

document.querySelector("#clearSearch").addEventListener("click", () => {
  state.search = "";
  searchInput.value = "";
  searchBox.classList.remove("has-value");
  searchInput.focus();
  renderModels();
});

document.querySelector("#freeFilter").addEventListener("click", (event) => {
  state.freeOnly = !state.freeOnly;
  event.currentTarget.setAttribute("aria-pressed", String(state.freeOnly));
  renderModels();
});

document.querySelectorAll(".filter-options").forEach((group) => {
  group.addEventListener("click", (event) => {
    const button = event.target.closest(".filter-pill");
    if (!button) return;
    group.querySelectorAll(".filter-pill").forEach((item) => item.classList.toggle("active", item === button));
    state[group.dataset.filter] = button.dataset.value;
    renderModels();
  });
});

document.querySelector("#sortSelect").addEventListener("change", (event) => {
  state.sort = event.target.value;
  renderModels();
});

grid.addEventListener("click", (event) => {
  const favorite = event.target.closest("[data-favorite]");
  if (favorite) {
    event.stopPropagation();
    const id = favorite.dataset.favorite;
    state.favorites.has(id) ? state.favorites.delete(id) : state.favorites.add(id);
    localStorage.setItem("sf-favorites", JSON.stringify([...state.favorites]));
    renderModels();
    showToast(state.favorites.has(id) ? "已加入收藏" : "已取消收藏");
    return;
  }
  const card = event.target.closest("[data-model-id]");
  if (card) openDrawer(models.find((model) => model.id === card.dataset.modelId), card);
});

grid.addEventListener("keydown", (event) => {
  if ((event.key === "Enter" || event.key === " ") && event.target.matches(".model-card")) {
    event.preventDefault();
    openDrawer(models.find((model) => model.id === event.target.dataset.modelId), event.target);
  }
});

activeFilters.addEventListener("click", (event) => {
  const button = event.target.closest("[data-remove-filter]");
  if (!button) return;
  const key = button.dataset.removeFilter;
  if (key === "freeOnly") {
    state.freeOnly = false;
    document.querySelector("#freeFilter").setAttribute("aria-pressed", "false");
  } else {
    state[key] = "全部";
    const group = document.querySelector(`[data-filter="${key}"]`);
    group.querySelectorAll(".filter-pill").forEach((item) => item.classList.toggle("active", item.dataset.value === "全部"));
  }
  renderModels();
});

document.querySelector("#resetFilters").addEventListener("click", resetFilters);
document.querySelector("#drawerBackdrop").addEventListener("click", () => closeDrawer());
drawer.addEventListener("click", (event) => {
  if (event.target.closest("#drawerClose")) closeDrawer();
  const copy = event.target.closest("[data-copy]");
  if (copy) copyText(copy.dataset.copy);
  const toastButton = event.target.closest("[data-toast]");
  if (toastButton) showToast(toastButton.dataset.toast);
});

document.addEventListener("click", (event) => {
  const toastButton = event.target.closest("[data-toast]");
  if (toastButton && !toastButton.closest("#modelDrawer")) showToast(toastButton.dataset.toast);
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && state.activeModel) closeDrawer();
  if (event.key === "/" && !/INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName)) {
    event.preventDefault();
    searchInput.focus();
  }
});

document.querySelector("#collapseSidebar").addEventListener("click", () => {
  document.body.classList.toggle("sidebar-collapsed");
  const collapsed = document.body.classList.contains("sidebar-collapsed");
  document.querySelector("#collapseSidebar").setAttribute("aria-label", collapsed ? "展开侧栏" : "收起侧栏");
});
document.querySelector("#mobileMenu").addEventListener("click", () => document.body.classList.add("mobile-nav-open"));
document.querySelector("#sidebarScrim").addEventListener("click", () => document.body.classList.remove("mobile-nav-open"));

renderModels();
iconRefresh();

const hashModel = decodeURIComponent(window.location.hash.replace(/^#model=/, ""));
if (hashModel && hashModel !== window.location.hash) {
  const model = models.find((item) => item.id === hashModel);
  if (model) openDrawer(model, document.querySelector(`[data-model-id="${CSS.escape(model.id)}"]`));
}
