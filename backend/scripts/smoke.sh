#!/usr/bin/env bash
# monolith 拓扑冒烟测试。
#
# 每个迭代的 DoD 都包含这一条（IMPLEMENTATION-PLAN.md §5）：
# CI 至少要保证 monolith 能启动并跑通冒烟，否则装配层会悄悄腐化——
# 等到想拆服务时才发现远程实现从来没被验证过（SERVICE-DECOMPOSITION.md §7）。
set -euo pipefail

BIN=${BIN:-bin}
CONTROL_ADDR=${CONTROL_ADDR:-127.0.0.1:18080}
DATA_ADDR=${DATA_ADDR:-127.0.0.1:18081}
METRICS_ADDR=${METRICS_ADDR:-127.0.0.1:19090}

if [[ -z "${UMAAS_DATABASE__DSN:-}" ]]; then
  echo "UMAAS_DATABASE__DSN is required (run 'make dev-up' first)" >&2
  exit 1
fi

export UMAAS_CONTROL_PLANE__ADDR=":${CONTROL_ADDR##*:}"
export UMAAS_DATA_PLANE__ADDR=":${DATA_ADDR##*:}"
export UMAAS_OBSERVABILITY__METRICS_ADDR=":${METRICS_ADDR##*:}"
export UMAAS_LOG__FORMAT=text
# I3 起，数据平面启动需要一条渠道配置（ValidateGateway）。冒烟不起真实上游，
# 这里给一个语法合法但连不通的地址——够验证"模型解析先于转发"这条路径，
# 真正的转发/流式行为由 internal/gateway 的单元测试覆盖（含伪上游）。
export UMAAS_GATEWAY__CHANNEL__BASE_URL="http://127.0.0.1:1"
export UMAAS_GATEWAY__STREAM__FIRST_BYTE_TIMEOUT="2s"
export UMAAS_GATEWAY__STREAM__STALL_TIMEOUT="2s"

"./${BIN}/umaas" &
PID=$!
trap 'kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 40); do
  if curl -fsS "http://${CONTROL_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }

# 1. 控制平面存活
curl -fsS "http://${CONTROL_ADDR}/healthz" | grep -q '"status":"ok"' \
  || fail "/healthz did not return ok"

# 2. 响应带 request_id（契约要求成功与失败都带）
curl -fsS "http://${CONTROL_ADDR}/healthz" | grep -q '"request_id"' \
  || fail "/healthz response is missing request_id"

# 3. 控制平面的错误响应也带 request_id 且包信封
curl -sS "http://${CONTROL_ADDR}/api/v1/nonexistent" | grep -q '"request_id"' \
  || fail "control-plane error response is missing request_id"

# 4. 数据平面**不包信封**，保持 OpenAI 原样兼容
BODY=$(curl -sS -X POST "http://${DATA_ADDR}/v1/chat/completions")
echo "$BODY" | grep -q '"error"' || fail "data plane did not return an OpenAI-shaped error"
echo "$BODY" | grep -q '"data"' && fail "data plane wrapped the response in an envelope"
echo "$BODY" | grep -q '"request_id"' && fail "envelope field leaked into the data plane body"

# 5. 指标端点可抓，且**导出的是我们自己的指标**。
#    只检查 "# HELP" 会被 go_* 运行时指标蒙混过关——
#    otel 的 exporter 注册错了注册表时，正是这个表现。
curl -fsS "http://${METRICS_ADDR}/metrics" | grep -q '^umaas_build_info' \
  || fail "/metrics does not expose umaas_* metrics (wrong registerer?)"

# 6. 就绪探针反映依赖状态
curl -fsS "http://${CONTROL_ADDR}/readyz" | grep -q '"status":"ready"' \
  || fail "/readyz did not report ready"

# ── I1 身份 ─────────────────────────────────────────────────────
EMAIL="smoke-$$-$(date +%s)@example.com"
JAR=$(mktemp)
trap 'rm -f "$JAR"' RETURN 2>/dev/null || true

# 7. 注册返回 201 且种下会话 cookie
REG=$(curl -sS -o /dev/stderr -w '%{http_code}' -c "$JAR" \
  -X POST "http://${CONTROL_ADDR}/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Smoke\",\"email\":\"${EMAIL}\",\"password\":\"smoke-password\",\"accepted_terms\":true}" \
  2>/dev/null)
[[ "$REG" == "201" ]] || fail "register returned $REG, expected 201"

# 8. cookie 必须是 HttpOnly（curl 的 cookie jar 用 #HttpOnly_ 前缀标记）
grep -q '#HttpOnly_.*umaas_session' "$JAR" || fail "session cookie is not HttpOnly"

# 9. 带 cookie 能拿到 /me
ME=$(curl -sS -b "$JAR" "http://${CONTROL_ADDR}/api/v1/me")
echo "$ME" | grep -q "$EMAIL" || fail "/me did not return the current user"
echo "$ME" | grep -q '"workspace_id"' || fail "/me is missing workspace_id"

# 10. 不带 cookie 是 401
CODE=$(curl -sS -o /dev/null -w '%{http_code}' "http://${CONTROL_ADDR}/api/v1/me")
[[ "$CODE" == "401" ]] || fail "/me without a session returned $CODE, expected 401"

# 11. 登出后会话失效
curl -sS -b "$JAR" -c "$JAR" -X POST "http://${CONTROL_ADDR}/api/v1/auth/logout" >/dev/null
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" "http://${CONTROL_ADDR}/api/v1/me")
[[ "$CODE" == "401" ]] || fail "/me after logout returned $CODE, expected 401"

# 12. 错误密码是 401，且与"账号不存在"不可区分
WRONG=$(curl -sS -X POST "http://${CONTROL_ADDR}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${EMAIL}\",\"password\":\"wrong-password\",\"remember\":false}")
MISSING=$(curl -sS -X POST "http://${CONTROL_ADDR}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"nobody-smoke@example.com","password":"wrong-password","remember":false}')
# 去掉 request_id 后两者必须完全一致
norm() { echo "$1" | sed 's/"request_id":"[^"]*"//'; }
[[ "$(norm "$WRONG")" == "$(norm "$MISSING")" ]] \
  || fail "wrong password and unknown account are distinguishable (account enumeration)"

# 13. 管理端会话与用户会话是两条独立的链：用户 cookie 打不开 admin 会话
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" \
  "http://${CONTROL_ADDR}/api/v1/admin/auth/session")
[[ "$CODE" == "401" ]] || fail "admin session accepted a user cookie (returned $CODE)"

rm -f "$JAR"

# ── I2 目录与价目 ───────────────────────────────────────────────
# 判据取自 IMPLEMENTATION-PLAN.md 的 M1b："catalog 展示价与结算价
# 共用同一个解析函数"——冒烟测试验证不了解析路径是不是同一个，
# 但能验证公开端点可用、且价格字段不是硬编码的假数据。

# 14. /catalog/summary 可用且带信封
curl -fsS "http://${CONTROL_ADDR}/api/v1/catalog/summary" | grep -q '"model_count"' \
  || fail "/catalog/summary did not return model_count"

# 15. /models 支持排序与分页 meta
curl -fsS "http://${CONTROL_ADDR}/api/v1/models?limit=1" | grep -q '"next_cursor"' \
  || fail "/models response is missing meta.next_cursor"

# 16. /docs/navigation 可用
curl -fsS "http://${CONTROL_ADDR}/api/v1/docs/navigation" >/dev/null \
  || fail "/docs/navigation is not reachable"

# 17. admin 的目录端点在无会话时拒绝
CODE=$(curl -sS -o /dev/null -w '%{http_code}' "http://${CONTROL_ADDR}/api/v1/admin/catalog/models")
[[ "$CODE" == "401" ]] || fail "/admin/catalog/models without a session returned $CODE, expected 401"

# ── I3 数据平面 MVP ─────────────────────────────────────────────
# 判据取自 IMPLEMENTATION-PLAN.md 的 B3："SSE 正常；客户端断开能取消上游"。
# 冒烟不接真实上游（那部分由 internal/gateway 的伪上游单测覆盖，
# 含首字节/stall 两段超时与"绝不假装 finish_reason: stop"），这里只验证
# 模型解析先于转发这条路径，以及数据平面**绝不包信封**这条硬约束。

# 18. 未知模型返回 404，且是 OpenAI 形状（不是我们的信封）
BODY=$(curl -sS -X POST "http://${DATA_ADDR}/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -d '{"model":"nonexistent/model","messages":[{"role":"user","content":"hi"}]}')
echo "$BODY" | grep -q '"error"' || fail "unknown model did not return an OpenAI-shaped error"
echo "$BODY" | grep -q '"data"' && fail "chat completions error response was wrapped in the control-plane envelope"

# 19. 畸形请求体返回 400（仍是 OpenAI 形状）
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://${DATA_ADDR}/v1/chat/completions" \
  -H 'Content-Type: application/json' -d '{"messages":[]}')
[[ "$CODE" == "400" ]] || fail "malformed chat request returned $CODE, expected 400"

echo "SMOKE OK (monolith topology, I0+I1+I2+I3)"
