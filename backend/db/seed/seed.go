// Package seed 把开发用的种子数据 embed 进二进制。
//
// 生产环境的目录由 admin 逐条录入（BILLING-AND-PRICING.md §3.6-①），
// `umaas seed catalog` 在 env=prod 时会直接拒绝执行。
package seed

import _ "embed"

//go:embed catalog.json
var Catalog []byte
