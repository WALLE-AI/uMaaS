package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/WALLE-AI/uMaaS/backend/internal/auth"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/domain"
	"github.com/WALLE-AI/uMaaS/backend/internal/platform"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres"
)

// runAdminCommand 实现 `umaas admin create`。
//
// 管理员账号只能由管理员创建，没有自助注册入口——**首个 super_admin 只能从
// 命令行创建**，这是首次部署的必经步骤。不写进部署文档，第一次上线会卡在
// 没人能登录（ARCHITECTURE.md §4.5）。
func runAdminCommand(args []string) error {
	// 动词必须在 flag 解析**之前**取出：Go 的 flag 包遇到第一个非 flag 参数
	// 就停止解析，`admin create --email x` 会导致所有 flag 都读不到。
	if len(args) == 0 {
		return errors.New("usage: umaas admin create --email <email> --name <name> [--super]")
	}
	verb, rest := args[0], args[1:]

	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	email := fs.String("email", "", "admin email")
	name := fs.String("name", "", "display name")
	super := fs.Bool("super", false, "create a super_admin (required for the first account)")
	role := fs.String("role", "operator", "role when --super is not set: operator|viewer")
	_ = fs.Parse(rest)

	if verb != "create" {
		return fmt.Errorf("unknown admin subcommand %q; usage: umaas admin create "+
			"--email <email> --name <name> [--super]", verb)
	}
	if *email == "" || *name == "" {
		return errors.New("--email and --name are required")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	keyBytes, err := cfg.EncryptionKeyBytes()
	if err != nil {
		return err
	}
	if len(keyBytes) == 0 {
		return errors.New("security.encryption_key is required to create an admin " +
			"(the TOTP secret must be encrypted at rest)")
	}
	cipher, err := platform.NewCipher(keyBytes)
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	adminRepo := postgres.NewAdminRepo(db)
	svc := auth.NewService(nil, adminRepo, postgres.NewAuditRepo(db), cipher)

	// --super 的门槛：第一个账号必须是 super_admin，之后再建 super_admin
	// 应当走管理界面（有操作者身份可审计），而不是绕过审计从命令行加。
	count, err := adminRepo.CountAdmins(ctx)
	if err != nil {
		return err
	}
	targetRole := domain.AdminRole(*role)
	if *super {
		targetRole = domain.AdminSuperAdmin
	}
	if count == 0 && targetRole != domain.AdminSuperAdmin {
		return errors.New("the first admin must be a super_admin; pass --super")
	}
	if count > 0 && *super {
		fmt.Fprintln(os.Stderr,
			"warning: creating an additional super_admin from the CLI bypasses the "+
				"in-app audit trail; prefer the admin console when possible")
	}

	password, err := readPassword()
	if err != nil {
		return err
	}

	enrollment, err := svc.CreateAdmin(ctx, auth.CreateAdminInput{
		Email: *email, Name: *name, Role: targetRole, Password: password,
	})
	if err != nil {
		return err
	}

	fmt.Printf(`
admin account created

  email : %s
  role  : %s

Two-factor authentication is MANDATORY and not yet enabled for this account.
Scan the URI below in an authenticator app, then confirm it once:

  %s

  (manual entry secret: %s)

Confirm with:

  curl -X POST http://<host>/api/v1/admin/auth/totp/confirm \
    -H 'Content-Type: application/json' \
    -d '{"email":"%s","password":"<password>","code":"<6-digit code>"}'

Until confirmed, this account cannot sign in.
`, enrollment.Account.Email, enrollment.Account.Role,
		enrollment.URI, enrollment.Secret, enrollment.Account.Email)

	return nil
}

// readPassword 从终端读密码，不回显。
//
// 不提供 --password 参数：命令行参数会进 shell 历史与 ps 输出，
// 而这是平台最高权限账号的密码。
func readPassword() (string, error) {
	fd := int(syscall.Stdin)
	if !term.IsTerminal(fd) {
		// 非交互环境（CI 引导）走标准输入，但明确提示。
		fmt.Fprintln(os.Stderr, "reading password from stdin (not a terminal)")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	fmt.Fprint(os.Stderr, "password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(os.Stderr, "confirm : ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}
