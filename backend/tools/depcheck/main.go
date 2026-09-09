// Command depcheck 断言 internal/ 下的模块依赖图是有向无环图，并检查模块边界。
//
// 为什么要有它（SERVICE-DECOMPOSITION.md §5-规则四）：
// **依赖成环是拆分的头号杀手，且它总是悄悄形成的**——某天有人为了图方便，
// 在 catalog 里 import 了 billing，编译通过、测试全绿，直到半年后想拆服务时
// 才发现两个模块已经互相缠死。放进 CI，让它在提交那一刻就红。
//
// 用 `go list` 而不是自己解析 import：go list 认识 build tag、认识条件编译，
// 手写解析器迟早在这两处出错。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const modulePrefix = "github.com/WALLE-AI/uMaaS/backend/"

// storeOwnership 表达规则一：模块之间只通过接口通信，
// 禁止跨模块直接访问对方的 store。
//
// 形式是"只有这些包可以 import store/<owner>"。I0 还没有分模块的 store，
// 先把机制建好——等 I2 的 catalog、I5 的 billing 进来时直接填表。
var storeOwnership = map[string][]string{
	// "internal/store/catalog": {"internal/catalog"},
	// "internal/store/billing": {"internal/billing"},
}

type pkg struct {
	ImportPath string
	Imports    []string
	Deps       []string
}

func main() {
	pkgs, err := listPackages()
	if err != nil {
		fatal(err)
	}

	var problems []string
	problems = append(problems, checkCycles(pkgs)...)
	problems = append(problems, checkStoreOwnership(pkgs)...)
	problems = append(problems, checkPlaneIsolation(pkgs)...)

	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "dependency check failed:")
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  -", p)
		}
		os.Exit(1)
	}
	fmt.Printf("dependency check passed (%d internal packages)\n", len(pkgs))
}

func listPackages() (map[string]*pkg, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}

	pkgs := map[string]*pkg{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		if strings.HasPrefix(p.ImportPath, modulePrefix) {
			pkgs[p.ImportPath] = &p
		}
	}
	return pkgs, nil
}

// checkCycles 找包级依赖环。
//
// Go 编译器本身就禁止 import 环，所以这里理论上永远不会触发——
// 它的真实价值是**在引入 interface 反转依赖之前先失败**：
// 有人为了绕开编译器的环检查而把类型塞进一个 shared/common 包时，
// 环会以"所有模块都依赖 common"的形式出现，那正是下面 checkStoreOwnership 要管的。
func checkCycles(pkgs map[string]*pkg) []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var problems []string
	var stack []string

	var visit func(string) bool
	visit = func(path string) bool {
		color[path] = gray
		stack = append(stack, path)
		defer func() { stack = stack[:len(stack)-1] }()

		p, ok := pkgs[path]
		if !ok {
			color[path] = black
			return false
		}
		for _, imp := range p.Imports {
			if !strings.HasPrefix(imp, modulePrefix) {
				continue
			}
			switch color[imp] {
			case gray:
				problems = append(problems, fmt.Sprintf(
					"import cycle: %s -> %s", strings.Join(stack, " -> "), imp))
				return true
			case white:
				if visit(imp) {
					return true
				}
			}
		}
		color[path] = black
		return false
	}

	paths := sortedKeys(pkgs)
	for _, p := range paths {
		if color[p] == white {
			visit(p)
		}
	}
	return problems
}

// checkStoreOwnership 实现规则三：一张表只有一个模块能写。
// 其他模块要改数据，走该模块的接口。这是将来"每服务一库"的前提。
func checkStoreOwnership(pkgs map[string]*pkg) []string {
	var problems []string
	for _, path := range sortedKeys(pkgs) {
		p := pkgs[path]
		rel := strings.TrimPrefix(path, modulePrefix)
		for _, imp := range p.Imports {
			impRel := strings.TrimPrefix(imp, modulePrefix)
			owners, guarded := storeOwnership[impRel]
			if !guarded {
				continue
			}
			if slicesContains(owners, rel) || rel == impRel {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s imports %s, which is owned by %s (SERVICE-DECOMPOSITION.md §5 rule 1/3)",
				rel, impRel, strings.Join(owners, ", ")))
		}
	}
	return problems
}

// checkPlaneIsolation 守住 ARCHITECTURE.md §1：控制平面与数据平面独立。
//
// gateway 不得 import httpapi 的信封包——包了信封就破坏 OpenAI 兼容性，
// 而这类错误在代码评审里极容易被放过（"复用一下不是挺好"）。
func checkPlaneIsolation(pkgs map[string]*pkg) []string {
	var problems []string
	const gateway = "internal/gateway"
	const envelope = "internal/httpapi/response"

	for _, path := range sortedKeys(pkgs) {
		rel := strings.TrimPrefix(path, modulePrefix)
		if !strings.HasPrefix(rel, gateway) {
			continue
		}
		for _, imp := range pkgs[path].Imports {
			if strings.TrimPrefix(imp, modulePrefix) == envelope {
				problems = append(problems, fmt.Sprintf(
					"%s imports %s: the data plane must not wrap responses in the control-plane "+
						"envelope (ARCHITECTURE.md §1)", rel, envelope))
			}
		}
	}
	return problems
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
