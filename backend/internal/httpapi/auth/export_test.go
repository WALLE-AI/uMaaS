package auth_test

import "github.com/WALLE-AI/uMaaS/backend/internal/platform"

// hashForTests 让测试能造一个真实的密码哈希，而不必导出生产代码里的内部函数。
func hashForTests(password string) (string, error) {
	return platform.HashPassword(password)
}
