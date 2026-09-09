package httpapi

import "github.com/WALLE-AI/uMaaS/backend/internal/domain"

// notImplemented 是骨架期的占位错误：端点尚未在本迭代实现。
//
// 用 404 而不是 501：契约里没有 501，且对客户端而言"这个端点还不存在"
// 与"路径写错了"是同一件事。等对应迭代实现后这个分支自然消失。
func notImplemented() error {
	return domain.NotFound("endpoint_not_implemented",
		"this endpoint is not implemented yet; see backend/IMPLEMENTATION-PLAN.md")
}
