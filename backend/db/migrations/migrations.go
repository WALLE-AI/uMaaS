// Package migrations 把 SQL 迁移 embed 进二进制。
//
// embed 是为了部署时不必额外带 SQL 文件，**不是**为了在服务启动时自动执行：
// 多副本同时启动会竞争迁移锁，且一次失败的迁移会让整个服务起不来
// （ARCHITECTURE.md §8）。生产环境用 `umaas-migrate up` 作为部署流水线的独立步骤。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
