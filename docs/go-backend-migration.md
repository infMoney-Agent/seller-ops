# 全业务后端迁移到 Go

## 最终运行结构

| 服务 | 目录 | 职责 |
| --- | --- | --- |
| 用户管理 | `apps/auth` | 原有 Go OIDC、会话、工作区、成员及用户管理 |
| 电商业务 | `apps/ecom` | Go 选品研究、文案生成、评论分析、竞品监控和广告分析 |
| 产品初筛 | `apps/screening` | Go 商品导入、分月数据、规则、复核、利润和导出 |
| 前端 | `apps/web` | Next.js 页面和验证会话后的同源签名代理 |

电商业务使用 Go HTTP 服务和 Go 监控 Worker。初筛使用 Go HTTP 服务、Go Worker 和 Go SQL 迁移命令。最终容器、CI 和部署脚本无需 Python、FastAPI、SQLAlchemy、Celery 或 Alembic 运行环境。Chromium 仍负责 Amazon 页面加载；模型通过各供应商 HTTP API 调用。

用户管理仍拥有 `seller_ops`，Casdoor 仍拥有自己的数据库，初筛独立拥有 `seller_screening`。竞品监控沿用原 Redis 账号命名空间。原 SQLite 数据卷保留，不自动删除其中的文件。

## 原模块兼容性

- 保留 `/api/v1/product-research/*`、`listing/*`、`reviews/*`、`monitor/*` 和 `ads/*` 对前端使用的路径与返回结构。
- 选品研究只处理当前榜单页面已读取的商品，直接使用模型综合评分；模型全部评分失败时返回明确错误。
- OpenAI、Anthropic、Gemini、Ollama 的适配在 Go 中实现，保留原模块提示词快照；文案按所选语言生成，运营分析使用中文。
- 监控继续使用 `seller:{user_id}:monitor:{asin}` 和 `seller:{user_id}:snapshots:{asin}`。不重命名或清空已有配置和快照，快照仍按最新优先、最多1000条保存。
- Go Worker 每分钟检查到期监控，尊重每条记录的监控间隔。Redis 分布式锁防止定时任务与手动采集重复执行。
- 广告模块改用独立广告凭据交换令牌。活动列表只有配置数据时明确说明缺少绩效数据，不据此生成出价或收益预测。

广告活动读取使用 Amazon 官方示例中的 Sponsored Products v3 列表接口，参考 [Amazon 官方 Postman 集合](https://github.com/amzn/ads-advanced-tools-docs/tree/main/postman)。本轮没有实现自动修改广告活动或发布商品。

## 身份转发

Next.js 每次验证原 Go 认证服务会话。电商代理增加 `ECOM_INTERNAL_SECRET`，签名覆盖时间、方法、含查询参数的路径和真实用户ID。Go 服务校验60秒时效和 HMAC，拒绝伪造用户头。

初筛继续使用独立 `SCREENING_INTERNAL_SECRET`，签名额外绑定工作区和角色。浏览器不能决定实际生效的用户、工作区或角色；写请求继续检查 Origin。Go 业务接口只在内部网络开放。

## 初筛数据库与旧任务接管

九张业务表和参数布局见 [数据库设计](product-screening-database.md)。SQL 迁移由 Go 二进制内嵌并顺序执行，已有 Alembic 版本只作为迁移起点读取，不执行 Python。

第五步迁移接管旧队列：保留旧载荷和结果，转换可恢复任务到 Go 队列格式，清除旧租约，使运行中的旧任务可重领。缺失必要快照的旧任务保留记录并标明失败原因。旧批量预览要求重新生成；已经执行的批次、人工历史和利润方案保留。

停止旧 Worker/Beat 后运行迁移，再启动 Go API 和 Worker。不要让两个运行时同时处理业务任务。

## 上线前配置

1. 备份 PostgreSQL、Redis 和初筛文件卷。
2. 在仓库外私有环境文件中增加 `ECOM_INTERNAL_SECRET`、`SCREENING_INTERNAL_SECRET`、`SCREENING_DB_PASSWORD`。两个签名密钥分别生成，至少32字节；密码使用足够长的 URL 安全随机值。
3. 检查原服务器覆盖配置，没有覆盖回旧 Python 命令或旧构建目录。
4. Go 初筛迁移只在 `screening-migrate` 执行。`screening-storage-init` 将初筛文件卷所有者统一为容器用户10001，不删除文件。
5. 构建 `apps/ecom` 和 `apps/screening` 镜像，启动新 API/Worker，确认健康检查，再验证登录、五个模块及一轮初筛流程。

根 Compose 已替换原运行入口，发布流水线增加独立 screening 镜像，保留现有 backend 镜像名称来承载 Go 电商镜像。旧 `ecom-beat` 容器在更新时作为孤立服务移除。

## 回滚边界

应用镜像回滚时保留数据库、Redis 和文件卷，不运行删表降级。已升级为九表的初筛库不能交给依赖已移除表的旧 Python 初筛代码；必要时暂时隐藏初筛入口，恢复原有其他模块，并保留新库等待修复。

本轮提交不合入 `dev`，不执行服务器上线。容器配置已进行静态验证；当前环境没有运行容器构建和完整 Compose 部署，因此上线前仍需执行镜像与容器验收。

## 已执行验证

- Go 认证模块独立 PostgreSQL 回归测试。
- Go 电商模块竞态测试、五模块契约、四个模型提供商请求/响应、网页解析和真实 Redis 兼容测试。
- Go 初筛真实 PostgreSQL 测试：新建与旧库接管、来源历史、父子体口径、批量复核与条件撤销、利润快照、导出、规则启用、乐观锁和任务租约恢复。
- Web 类型检查、生产构建、涉及文件的 lint，以及生产构建连接 Go 测试服务的浏览器验收。
- 十万商品、五十万观察记录暖态分页：20次样本，三个场景 P95 约为234ms、241ms、372ms。测量包含 Go 查询和数据组装，不包含公网传输和浏览器渲染；环境为该服务器的隔离 PostgreSQL14测试实例。

真实模型和 Amazon 凭据调用未执行；适配器使用模拟 HTTP 服务和页面样本验证。不能将此测试报告解释为真实 Amazon 采集可用性或生产部署验收通过。

全仓 Web lint 仍有原有规则错误，本轮涉及的15个前端文件检查为零错误、零警告；CI 对本轮修改路径执行检查，其他 Web 构建与类型检查保持覆盖。
