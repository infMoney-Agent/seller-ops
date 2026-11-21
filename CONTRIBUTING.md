# 参与 Seller Ops 开发

先阅读 [AGENTS.md](AGENTS.md)。该文件是团队共同约定，适用于人工开发和 AI 编程助手。

## 环境与分支

使用 Node.js 22 或更新的受支持版本；前端依赖按 apps/web/bun.lock 冻结安装。从最新 dev 创建功能分支，通过 PR 合入，保留既有必需检查。业务数据库与账号数据库隔离，测试使用专用数据源。

## 前端开发流程

1. 阅读项目内的 [Modern Web Guidance 技能入口](.agents/skills/modern-web-guidance/SKILL.md)。
2. 根目录运行 `npm run guidance -- search "具体英文交互或布局需求"`；一次只查一个主题，取得相关 ID 后用 `npm run guidance -- retrieve "ID"` 阅读。
3. 按项目组件体系实施，检查浏览器兼容、键盘及焦点、错误/空/加载状态；仅测试本次改动涉及的行为。
4. 运行 `npm run guidance:check`，并按改动范围完成类型、lint、构建或交互检查；在 PR 模板中记录采用的指南和实际验证。

首次查询需要网络下载固定版本 CLI；之后可使用 npm 缓存，或离线阅读仓库 guides。没有相关指南时写明原因，不检索无关内容凑流程。Go 后端、SQL、部署和纯文档改动不必执行前端指南检索。

## 评审要求

评审者核对指南是否适用于实际改动、回退是否保留、验证证据是否充分。CI 检查接入文件、版本锁和指南内容完整性；它不能自动证明所有界面符合指南。命令、跨工具入口与升级方式见 [团队 Web 开发指南](docs/web-development-guidance.md)。
