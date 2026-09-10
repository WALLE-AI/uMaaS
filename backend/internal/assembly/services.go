// Package assembly 是拓扑装配层：同一份代码支持 monolith 与 split 两种部署形态。
//
// 这是让「现在不拆」与「将来能拆」同时成立的技术核心（SERVICE-DECOMPOSITION.md §4）。
// 所有跨服务调用都走接口，接口有两种实现：进程内直调 / gRPC 客户端。
// 启动时按配置装配，业务代码一行不改——拆分是配置决策而非重构。
//
// # 三条必须守住的约束
//
//  1. 接口必须是"可远程化"的形状：参数与返回值要能序列化，不能传 *pgx.Tx、
//     不能传闭包、不能依赖调用方的内存状态。这个约束从第一天就要守，事后改代价极大。
//  2. 本地实现能参与同一事务，远程实现不能。这是最危险的隐藏差异——单机跑得好好的
//     事务语义，切多进程后静默变成最终一致。本包用 TransactionalService 显式标记
//     这类操作，并在装配期拒绝把它们远程化。
//  3. 错误语义要统一：远程实现必须把 gRPC status 映射回同一组 domain 错误。
package assembly

import (
	"fmt"

	"github.com/WALLE-AI/uMaaS/backend/internal/catalog"
	"github.com/WALLE-AI/uMaaS/backend/internal/catalogadmin"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/pricing"
)

// Services 是全部跨模块服务的集合。模块之间只通过这里的接口通信，
// 禁止跨模块直接访问对方的 store（SERVICE-DECOMPOSITION.md §5-规则一）。
type Services struct {
	Topology config.Topology

	// Catalog 是目录的只读服务（I2 / B2）。**它可以远程化**：
	// 全是只读查询，没有跨模块事务，是最先能被拆出去的一块。
	Catalog *catalog.Service
	// Pricing 是价目解析器（I2 / M1）。展示侧与结算侧共用同一个实例——
	// 这是"展示价与计费价同源"在装配层的落点（BILLING-AND-PRICING.md §3.6-②）。
	//
	// **注意它现在不是 TransactionalService**：解析价目只是读。
	// I5 的结算服务才是，那时要把它加进 transactionalServices()。
	Pricing *pricing.Resolver
	// CatalogAdmin 是目录与价目的写入侧（I2 / M1a）。
	CatalogAdmin *catalogadmin.Service

	// 后续迭代逐个填充：
	//   Quota   QuotaService     // I5，计费。TransactionalService，永不远程
	//   Infra   InfraService     // I7
}

// TransactionalService 由"要求事务原子性"的服务实现。
//
// 计费是典型：一次推理要查配额 → 预扣 → 转发 → 结算，这条链路若跨越服务边界，
// 要么引入 Saga/TCC，要么接受最终一致（意味着可以超额、产生坏账）。
// BILLING-AND-PRICING.md §5 与 SERVICE-DECOMPOSITION.md §1 都已定死：计费不跨服务边界。
//
// 实现了这个接口的服务，在 split 拓扑下**装配期就会被拒绝**，而不是等到线上发现对不上账。
type TransactionalService interface {
	// RequiresLocalTransaction 返回 true 表示该服务必须与调用方同进程。
	RequiresLocalTransaction() bool
	// ServiceName 用于装配期报错时定位。
	ServiceName() string
}

// Deps 是装配需要的外部依赖。由 main 注入，避免 assembly 依赖 store——
// 那会让"装配层决定拓扑"变成"装配层知道数据库"。
type Deps struct {
	Catalog      *catalog.Service
	Pricing      *pricing.Resolver
	CatalogAdmin *catalogadmin.Service
}

// Build 按配置装配服务集合。
func Build(cfg *config.Config, deps Deps) (*Services, error) {
	svcs := &Services{
		Topology:     cfg.Topology,
		Catalog:      deps.Catalog,
		Pricing:      deps.Pricing,
		CatalogAdmin: deps.CatalogAdmin,
	}

	switch cfg.Topology {
	case config.TopologyMonolith:
		// 进程内直调：零序列化、零网络、可参与同一个数据库事务。
	case config.TopologySplit:
		// gRPC 客户端：跨进程，带超时/重试/熔断。
		// 装配前先校验没有事务型服务被远程化。
		if err := guardTransactional(svcs); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("assembly: unknown topology %q", cfg.Topology)
	}
	return svcs, nil
}

// guardTransactional 在启动期拒绝把要求事务原子性的服务远程化。
//
// 这条检查存在的意义：单机跑通的事务语义在切到 split 后会静默降级为最终一致，
// 而症状是"偶尔超额扣费"——最难查的一类 bug。宁可起不来。
func guardTransactional(s *Services) error {
	return guardTransactionalList(s, s.transactionalServices())
}

// guardTransactionalList 与 guardTransactional 分开，是为了让这条规则可以被单独测试——
// 在 Quota 之类的服务真正接进来之前，它是唯一能验证这条保障有效的方式。
func guardTransactionalList(_ *Services, svcs []TransactionalService) error {
	for _, svc := range svcs {
		if svc.RequiresLocalTransaction() {
			return fmt.Errorf(
				"assembly: service %q requires local transactions and cannot run in split topology; "+
					"see SERVICE-DECOMPOSITION.md §1 (billing must not cross service boundaries)",
				svc.ServiceName())
		}
	}
	return nil
}

// transactionalServices 列出所有实现了 TransactionalService 的服务。
// 随着服务被填进 Services，这里要同步维护——I5 加入 Quota 时是第一个。
func (s *Services) transactionalServices() []TransactionalService {
	var out []TransactionalService
	// 例：if s.Quota != nil { out = append(out, s.Quota) }
	return out
}
