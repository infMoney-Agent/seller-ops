# Go 业务后端与三进程部署

应用按职责保留三个常驻进程：`apps/auth` 登录服务（8080）、`apps/backend` 业务后端（8000）、`apps/web` 前端（3000）。基础设施和部署时的短期迁移命令单独运行。

业务后端是一个 Go module、一个二进制和一个 HTTP listener。`internal/ecom` 与 `internal/screening` 是内部包，不能独立执行。监控任务和初筛任务在主进程内运行；任务异常退出会停止该业务进程，由部署系统重启，避免 API 看似正常但队列无人消费。

## API 和数据库边界

- `/api/v1/screening/*` 交给初筛包，`/api/v1/*` 的其他业务交给电商包；两者使用同一端口。
- Next.js 继续验证真实登录会话、写请求 Origin，并为内部业务请求签名；原浏览器路径不变。
- `/health`、`/healthz` 同时检查初筛 PostgreSQL 和电商 Redis。签名、账号和租户校验仍由对应业务包执行。
- `/openapi.json` 为原电商契约，`/screening/openapi.json` 为初筛契约。
- 初筛独立数据库和账号、复合租户外键、文件存储及所有历史数据保持不变。用户管理库不迁入业务库。

## 命令

在 `apps/backend` 构建：`go build -o seller-backend .`。

- `seller-backend migrate`：部署时执行内嵌迁移，结束后退出；不启动任务或 Redis 客户端。
- `seller-backend serve`：唯一常驻业务命令，同时启动接口、监控及初筛任务。
- `seller-backend health`：检查配置端口上的聚合健康接口。

不再存在独立的 `worker` 启动命令、两个旧 Go module、独立电商/初筛 Dockerfile 或 screening 镜像。生产使用一个 backend 镜像；原文件与数据库卷继续挂载。

## 升级与回滚

先备份数据库、Redis 及文件卷。停止旧独立 Worker，执行数据库初始化与迁移，再启动统一后端。`docker compose up --remove-orphans` 移除旧服务容器，不删除数据卷。

回滚使用已有应用镜像和原有数据卷，保留已应用的增量迁移，不执行删表降级。九表初筛库不能交给依赖已移除旧表的 Python 初筛程序。旧版整栈的健康检查仍由发布助手兼容处理。

CI 保留 `Dev CI required` 合并门禁，验证认证、统一业务和 Web。模型及 Amazon 调用的离线测试使用合成响应；真实外部凭据不作为测试必需项。
