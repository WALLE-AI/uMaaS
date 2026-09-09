# uMaaS backend
#
# 迭代 I0（骨架与纪律）。任务分解见 IMPLEMENTATION-PLAN.md。

SHELL := /bin/bash
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
BIN := bin

.PHONY: help
help: ## 列出可用目标
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## 构建两个二进制
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/umaas ./cmd/umaas
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/umaas-migrate ./cmd/umaas-migrate

.PHONY: test
test: ## 单元测试（不碰数据库，毫秒级）
	$(GO) test -race ./...

.PHONY: test-integration
test-integration: ## 集成测试（testcontainers 起真实 Postgres）
	$(GO) test -race -tags integration -count=1 ./...

.PHONY: vet
vet: ## go vet，含 integration tag 下的代码
	$(GO) vet ./...
	$(GO) vet -tags integration ./...

.PHONY: fmt
fmt: ## 格式化
	$(GO) fmt ./...

.PHONY: sqlc
sqlc: ## 由 db/queries 重新生成类型安全的查询代码
	sqlc generate

.PHONY: check-sqlc
check-sqlc: ## 断言生成代码与 db/queries 一致（防止手改生成代码）
	@sqlc generate
	@git diff --exit-code internal/store/postgres/dbgen || \
		(echo "generated code is out of date; run 'make sqlc' and commit" && exit 1)

.PHONY: check-deps
check-deps: ## 断言模块依赖图是 DAG（SERVICE-DECOMPOSITION.md §5-规则四）
	$(GO) run ./tools/depcheck

.PHONY: smoke
smoke: build ## monolith 拓扑冒烟测试（每个迭代的 DoD）
	./scripts/smoke.sh

.PHONY: ci
ci: vet test check-deps ## CI 跑的全部检查（不含需要 Docker 的集成测试）

.PHONY: dev-up
dev-up: ## 起本地依赖（Postgres + Redis）
	docker compose -f deploy/docker-compose.dev.yml up -d

.PHONY: dev-down
dev-down: ## 停本地依赖
	docker compose -f deploy/docker-compose.dev.yml down

.PHONY: migrate
migrate: build ## 迁移到最新版本
	./$(BIN)/umaas-migrate up

.PHONY: clean
clean:
	rm -rf $(BIN)
