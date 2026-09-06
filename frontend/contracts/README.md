# 仓库级 API 契约

`openapi.yaml` 是后端 HTTP 契约的**唯一真相源**，由两个前端应用共用：

- `frontend/web` — 开发者门户与用户自助控制台
- `frontend/admin` — 平台管理控制台

它原先放在 `web/src/api/` 下，位置暗示"这是 web 的东西"，但实际上是两端共同的约定。
移到这里是为了让归属正确。

## 约定

- 两端各自维护面向自己的 TS 类型（`web/src/api/contracts.ts`、`admin/src/api/contracts.ts`），
  但都必须与本文件对齐。规模再大时再考虑用 openapi 代码生成。
- 信封、错误码语义、分页与缓存策略见 `web/src/api/README.md`。
- 管理端端点（`/admin/*`）尚未写入本文件，待后端确认后补充。
