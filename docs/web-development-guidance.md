# 团队 Web 开发指南

本项目接入 [GoogleChrome/modern-web-guidance](https://github.com/GoogleChrome/modern-web-guidance)。所有人工开发和编程助手遵循根目录 [AGENTS.md](../AGENTS.md) 与 [CONTRIBUTING.md](../CONTRIBUTING.md)。指南仅作为开发工具，不进入浏览器包或 Go 服务运行依赖。

## 当前版本与入口

- 本地技能：`.agents/skills/modern-web-guidance/SKILL.md`，完整指南位于同目录 guides。
- 上游提交及逐文件 SHA-256：`docs/modern-web-guidance.lock.json`。CLI 固定为 1.0.1，要求 Node.js 22+。
- 上游使用 Apache-2.0，许可证随技能保存。技能通过 skill-installer 从固定 Git 提交安装；许可证来自官方固定版本 npm 发行包。项目现有其他技能继续由 skills-lock.json 管理，新指南的完整性使用独立版本锁。

在仓库根目录执行：

```sh
npm run guidance -- search "responsive data table"
npm run guidance -- retrieve "实际搜索返回的ID"
npm run guidance:check
```

Windows 使用相同 npm run 命令。查询与指南 ID 以实际返回为准，不把示例占位符作为 ID。首次执行需要下载固定版 CLI；已缓存时可以用 `npx --offline --yes modern-web-guidance@1.0.1 search "responsive data table"`。离线也可直接搜索、阅读仓库的 Markdown；在 PR 说明使用了本地快照。

## 团队工作流

界面、CSS 和浏览器逻辑改动先用 2–5 个英文单词描述单一能力，搜索匹配的指南，再 retrieve 阅读对应规则、陷阱及回退要求。按项目 React/Next.js 体系实现，完成相关验证，并在 PR 模板列出采用 ID、对应文件、具体处理及证据。无适用指南时说明原因；后端或纯文档改动不必检索。

面向当前稳定版 Chrome、Edge、Firefox、Safari。默认采用 Baseline Widely available 的能力；较新功能必须检测支持并提供可用回退。保留共享 UI 所需的焦点、键盘和 Portal 行为，避免为了新 API 重写成熟组件。必要时验证窄窗口、键盘操作、空数据和错误状态；不机械地对每个文案调整跑全量测试。

`legacy-scan` 仅在明确开展整体旧模式排查时运行，结果需结合保留/替换条件人工复核。它只覆盖部分规则，不能当成全部页面的合规或性能证明。

## 编程助手接入

- 支持项目技能发现的助手：从 `.agents/skills` 发现技能，并遵循根目录 AGENTS.md；下次进入项目会话时读取新增规则。
- GitHub Copilot：`.github/copilot-instructions.md` 指向同一项目约定。
- Claude Code：根目录 CLAUDE.md 引入 AGENTS.md。
- Cursor：`.cursor/rules/web-guidance.mdc` 对 apps/web 文件生效。
- 其他工具：将 AGENTS.md 与技能入口加入项目上下文。是否自动加载取决于工具及其设置，不能靠仓库文件保证所有客户端都已加载。

## 检查与评审

`npm run guidance:check` 无需网络，检查固定 CLI、共享入口、PR 模板、CI 接入和随仓库指南的完整性。该命令已接入 Web CI，继承现有 Dev CI required 合入要求。它检查接入是否完整，不宣称能证明开发者已正确应用所有指南。

评审者必须核对 PR 指出的实际代码及验证证据。只有新增能力需要时才采用对应指南，不改变既有租户隔离、人工复核保留、来源追溯或权限校验行为。

## 升级

1. 在独立提交或 PR 选择新的上游完整 commit 与 CLI 精确版本，审阅其 SKILL.md、指南变更及许可证。
2. 更新 vendored 技能快照；不要使用安装器覆盖已有目录而不检查差异，也不要通过通用 skills update 悄悄替换本快照。
3. 更新版本锁中的 commit、cliVersion、必要的 licenseSource，并同步 package.json 的 guidance 命令；再执行 `node scripts/check-web-guidance.mjs --write-lock` 重算文件摘要。
4. 运行 `npm run guidance:check`、实际 search/retrieve，并评审差异。更新不代表需要批量重写现有前端。
