#!/usr/bin/env node
/**
 * 比对 web 与 admin 的共同依赖版本是否一致。
 *
 * 方案 §9.0 定的规矩是「技术栈统一、代码不共享」。不引入 workspaces 就意味着
 * 两个 package.json 各写一份版本号 —— 靠自觉维护对齐必然会漂，漂了之后会表现为
 * 「web 能跑 admin 跑不了」这类最难查的问题。所以用脚本兜住。
 *
 * 用法：node frontend/scripts/check-versions.mjs
 * CI 里挂上即可，不一致时以非零退出码失败。
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')

const apps = ['web', 'admin'].map((name) => {
  const pkg = JSON.parse(readFileSync(join(root, name, 'package.json'), 'utf8'))
  return {
    name,
    deps: { ...(pkg.dependencies ?? {}), ...(pkg.devDependencies ?? {}) },
  }
})

const [web, admin] = apps
const shared = Object.keys(web.deps).filter((dep) => dep in admin.deps)

const mismatches = shared
  .filter((dep) => web.deps[dep] !== admin.deps[dep])
  .map((dep) => ({ dep, web: web.deps[dep], admin: admin.deps[dep] }))

console.log(`共同依赖 ${shared.length} 个`)

if (mismatches.length === 0) {
  console.log('✓ web 与 admin 的共同依赖版本完全一致')
  process.exit(0)
}

console.error(`\n✗ 发现 ${mismatches.length} 处版本不一致：\n`)
for (const item of mismatches) {
  console.error(`  ${item.dep}`)
  console.error(`    web:   ${item.web}`)
  console.error(`    admin: ${item.admin}`)
}
console.error('\n技术栈应保持统一（§3.1）。请对齐后重试。')
process.exit(1)
