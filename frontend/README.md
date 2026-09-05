# uMaaS Frontend

uMaaS（Unified Model as a Service）是一个统一的 AI 模型接入、路由、评测与运营平台。它将不同厂商的文本、图像、语音和多模态模型整合到一致的 API 与控制界面中，帮助开发者根据质量、价格、速度、上下文长度和服务可用性选择模型，而不必为每个供应商维护独立的接入逻辑。

本目录是 uMaaS 的前端产品原型，交互和信息架构参考了模型聚合平台的高密度工作界面，并使用独立的 uMaaS 品牌、示例数据和视觉系统。

## 产品能力

- **统一模型目录**：集中浏览不同厂商模型，支持搜索、模态、上下文、价格、能力和供应商筛选。
- **模型比较**：查看上下文窗口、输入输出价格、吞吐速度、调用量和发布时间，并选择模型进行比较。
- **模型详情**：提供 Providers、Pricing、Performance、Uptime、Benchmarks、Apps、Activity 和 FAQ 等分区。
- **智能路由**：按照质量、成本、速度、区域及供应商健康状态选择请求端点，并支持故障转移。
- **独立评测**：从质量、价值和速度三个维度展示可复现的模型评测及其配置、成本和遥测信息。
- **使用量排行**：按模型、厂商、任务、编程语言、工具调用、图像和应用分析真实调用趋势。
- **开发者文档**：提供 API 快速开始、认证、模型路由、工具调用和多模态接入示例。
- **Agent Harness**：为 Claude Code、Codex CLI、OpenCode 和 Aider 等工具生成模型与路由配置。
- **账户认证**：包含邮箱登录注册，以及可配置的 GitHub 和 Google OAuth 跳转流程。

## 页面结构

| 路由 | 功能 |
| --- | --- |
| `/` | 产品首页、平台指标和智能路由演示 |
| `/models` | 模型目录、组合筛选、收藏、比较和视图切换 |
| `/models/:provider/:model` | 模型供应商、价格、性能、可用性和评测详情 |
| `/benchmarks` | 智能体、推理和搜索评测榜 |
| `/rankings` | 多模态使用趋势与 13 类排行榜数据 |
| `/docs` | API 快速开始、代码示例和分组文档目录 |
| `/harness` | Agent Harness 安装与配置生成器 |
| `/login` | 邮箱、GitHub 和 Google 登录 |
| `/signup` | 邮箱、GitHub 和 Google 注册 |

## 技术栈

- React 19 + TypeScript
- Vite
- Tailwind CSS 4
- Ant Design 与 Ant Design Icons
- React Router
- Recharts

页面支持桌面端和移动端响应式布局。厂商 Logo 保存在 `public/logos`，避免页面运行时依赖第三方图片服务。

## 本地运行

环境要求：Node.js 20 或更高版本。

```bash
npm install
npm run dev
```

开发服务器默认运行于 `http://127.0.0.1:5173/`。生产构建：

```bash
npm run build
```

构建结果输出到 `dist/`。

## OAuth 配置

将 `.env.example` 复制为 `.env`，并将地址设置为后端提供的 OAuth 发起端点：

```bash
VITE_GITHUB_OAUTH_URL=https://api.example.com/auth/github
VITE_GOOGLE_OAUTH_URL=https://api.example.com/auth/google
```

前端会附加以下参数：

- `mode`：当前流程为 `login` 或 `signup`。
- `return_to`：认证完成后的前端返回地址。

OAuth Client Secret、授权码交换、账户绑定和会话签发必须由后端完成，不能放入 Vite 环境变量或浏览器代码。

## 当前状态

当前实现是可交互的前端产品原型，模型数据、排行榜、评测结果、供应商状态和登录提交结果均为演示数据。接入生产环境时还需要连接模型目录 API、计费与遥测服务、排行榜数据源、身份认证后端和持久化存储。

## 目录说明

```text
frontend/
├── public/logos/       # 模型厂商 Logo
├── src/App.tsx         # 页面、路由和主要交互
├── src/data.ts         # 示例模型与榜单数据
├── src/index.css       # 全局视觉系统和响应式样式
├── .env.example        # OAuth 环境变量示例
└── package.json        # 依赖与开发脚本
```
