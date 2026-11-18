# Seller Ops · 电商运营工作台

Seller Ops 是一个自托管的亚马逊运营工作台。登录后进入简体中文仪表盘，可使用选品研究、商品页面生成、评论分析、竞品监控和广告优化。账号登录由 Casdoor 提供。

## 功能与当前状态

| 模块 | 实现与使用条件 |
| --- | --- |
| 选品研究 | 用 Playwright 读取亚马逊美国站畅销榜，在 10 个预设类目中逐件调用模型评分，再按综合分筛选和排序。需要模型配置和畅销榜访问。 |
| 商品页面生成 | 根据关键词和商品信息生成标题、五点描述、商品描述、搜索词和 A+ 草稿，也可按 ASIN 读取商品后优化。支持六种输出语言，需要模型配置，生成的草稿可复制到卖家平台。 |
| 评论分析 | 按 ASIN 读取评论并归纳差评痛点、好评亮点与商品改进建议。需要访问亚马逊评论页面和配置模型。 |
| 竞品监控 | 按登录账号保存监控列表和历史记录。支持手动记录，后台每小时检查到期的 ASIN。使用 Redis、Celery 和 Playwright。 |
| 广告优化 | 读取广告活动并生成出价建议，需要 Amazon Advertising 账户授权和模型配置。当前适配器的花费、销售额等指标仍是占位值，真实投放表现需要进一步接入。 |

选品研究读取榜单当前页面，未实现翻页，也未提取评论数。综合分来自模型，结果只展示在本次页面里。产品初筛支持卖家精灵文件导入、产品库和人工复核，使用独立 Go 服务。模型未配置或全部评分失败时返回明确错误。选品、评论和广告的模型分析使用简体中文，商品页面文案按所选语言生成。

## 项目结构

~~~text
seller-ops/
├── apps/
│   ├── web/       # Next.js 前端和同域 API 代理
│   ├── auth/      # Go 登录管理、工作区与成员权限
│   ├── ecom/      # Go 电商业务、采集、模型调用和监控任务
│   └── screening/ # Go 初筛、独立业务库、迁移和持久化任务
├── docs/          # 项目部署文档
└── package.json   # 根目录脚本代理到 apps/web
~~~

Casdoor 初始化文件位于 [apps/auth/casdoor](apps/auth/casdoor)。Go 服务通过 OIDC 接入 Casdoor，Casdoor 进程由 Compose 或宿主机启动。

浏览器通过 Next.js 验证 Casdoor 会话并签名访问内部 Go 服务。用户管理和 Casdoor 使用各自 PostgreSQL 数据库，初筛使用独立 seller_screening 业务库；竞品列表和快照沿用 Redis。原 SQLite 数据卷保留。业务后端不再需要 Python 运行环境。

## 启动方式

有 HTTPS 域名时，按 [Casdoor 部署说明](docs/deployment.md) 初始化身份服务，再以 Compose 的 app 配置组启动整套应用。

### 通过 SSH 转发运行 Compose 测试环境

准备 Docker Engine 和 Docker Compose 2.24.4 或更新版本。在服务器的项目根目录执行：

~~~bash
mkdir -p -m 700 "$HOME/.config/seller-ops-local"
env_file="$HOME/.config/seller-ops-local/compose.env"

if [ ! -e "$env_file" ]; then
  (
    umask 077
    cat > "$env_file" <<ENV
APP_DOMAIN=localhost:18443
AUTH_DOMAIN=localhost:18444
POSTGRES_PASSWORD=$(openssl rand -hex 32)
APP_DB_PASSWORD=$(openssl rand -hex 32)
AUTH_DB_PASSWORD=$(openssl rand -hex 32)
SCREENING_DB_PASSWORD=$(openssl rand -hex 32)
SCREENING_INTERNAL_SECRET=$(openssl rand -hex 32)
OIDC_CLIENT_ID=
OIDC_CLIENT_SECRET=
ENV
  )
fi

seller_local() {
  docker compose --env-file "$HOME/.config/seller-ops-local/compose.env" \
    -p seller-local -f compose.yaml -f apps/auth/casdoor/compose.local.yaml "$@"
}

seller_local up -d db casdoor proxy
~~~

在自己电脑的 PowerShell 或终端中建立 SSH 转发。将示例账号和主机替换为实际值，并保持命令运行。

~~~powershell
ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:18443:127.0.0.1:18443 -L 127.0.0.1:18444:127.0.0.1:18444 user@server.example
~~~

1. 打开 [Casdoor 管理台](http://localhost:18444)，修改初始管理员密码，创建组织、OIDC 应用和邮箱已验证的账号。关闭公开注册，将回调地址设为 `http://localhost:18443/api/auth/callback`。OIDC 应用应签发包含 `iss`、`sub`、`nonce`、`email` 和 `email_verified` 声明的 ID Token。
2. 将 OIDC 客户端 ID 和密钥填写到服务器的 `compose.env`。需要真实评分或生成时，按下节填写模型配置。
3. 在项目根目录运行以下命令。重新开启服务器终端后，先重新定义上面的 `seller_local` 函数。

~~~bash
seller_local --profile app config --quiet
seller_local --profile app up -d --build
seller_local --profile app ps
~~~

打开 [Seller Ops](http://localhost:18443)。需要工作区时，所有者先使用已验证邮箱登录一次，再将示例邮箱换成该账号的邮箱并创建工作区。

~~~bash
seller_local --profile app exec api seller-server create-workspace "运营测试" owner@example.com
~~~

查看服务日志或停止测试环境：

~~~bash
seller_local --profile app logs --tail=100 web api casdoor ecom-api ecom-worker screening-api screening-worker ecom-redis
seller_local --profile app down
~~~

### 模型和亚马逊配置

在部署使用的私有环境文件里设置所需变量。[EcomAgent 环境变量示例](apps/ecom/.env.example)

| 场景 | 配置 |
| --- | --- |
| OpenAI | `LLM_PROVIDER=openai`，设置 `OPENAI_API_KEY`，可用 `OPENAI_MODEL` 指定模型 |
| Anthropic 或 Gemini | `LLM_PROVIDER=anthropic` 或 `gemini`，分别设置 `ANTHROPIC_API_KEY` 或 `GEMINI_API_KEY` |
| Ollama | `LLM_PROVIDER=ollama`，设置业务服务可访问的 `OLLAMA_BASE_URL`。容器中的 `localhost` 指向该容器 |
| 亚马逊网页采集 | 业务服务需可访问 Amazon，Compose 的电商镜像会安装 Chromium |
| 广告优化 | 配置 `AMAZON_ADS_CLIENT_ID`、`AMAZON_ADS_CLIENT_SECRET`、`AMAZON_ADS_REFRESH_TOKEN` 和 `AMAZON_ADS_PROFILE_ID`；活动配置缺少绩效数据时显示待补充 |

同一部署使用共享的模型和亚马逊配置。更新 Compose 环境文件后，重新执行 `seller_local --profile app up -d` 让配置生效。

### Ubuntu 原生进程

已运行 PostgreSQL、Casdoor 和 Redis 的服务器可以直接启动进程。Go 版本见 [go.mod](apps/auth/go.mod)，Web 构建镜像使用 Node.js 22，三个后端使用 Go 1.25，电商采集镜像另带 Chromium。下表的地址对应上面的 SSH 转发命令。

| 服务 | 启动目录与命令 | 配置 |
| --- | --- | --- |
| Casdoor | Casdoor 安装目录，运行 `./casdoor` | 监听 `127.0.0.1:18444`，origin 为 `http://localhost:18444` |
| Go | `apps/auth`，运行 `go run .` | `SELLER_LISTEN_ADDR=127.0.0.1:8080`、`APP_ORIGIN=http://localhost:18443`、`OIDC_ISSUER=http://localhost:18444`，以及 PostgreSQL 的 `DATABASE_URL` 和 OIDC 凭据 |
| Go 电商 | `apps/ecom`，运行 `go run . serve`，另一个进程运行 `go run . worker` | `ECOM_ADDR=127.0.0.1:18080`、`ECOM_INTERNAL_SECRET`、`REDIS_URL` 和模型配置 |
| Go 初筛 | `apps/screening`，先 `go run . migrate`，再分别运行 `go run . serve` 与 `go run . worker` | `SCREENING_ADDR=127.0.0.1:18090`、独立 `SCREENING_DATABASE_URL`、`SCREENING_STORAGE_DIR`、`SCREENING_INTERNAL_SECRET` |
| Next.js | `apps/web`，运行 `npm run dev -- --hostname 127.0.0.1 --port 18443` | `SELLER_API_URL=http://127.0.0.1:8080`、`SELLER_APP_ORIGIN=http://localhost:18443`、`ECOM_API_URL=http://127.0.0.1:18080` |

Go 电商采集需要系统 Chromium，可用 `CHROME_BIN` 指定路径。Next.js 额外设置 `SCREENING_API_URL=http://127.0.0.1:18090` 和两个内部签名密钥，分别与对应服务一致。Redis 绑定回环并启用持久化。已验证邮箱的用户登录后，可在 `apps/auth` 运行 `go run . create-workspace "运营测试" owner@example.com` 创建工作区。

## 开发与检查

以下 Web 命令在项目根目录运行，开发服务默认位于 [localhost:3000](http://localhost:3000)。需要验证登录或业务请求时，按上一节配置后端和一致的 OIDC 回调地址。

~~~bash
(cd apps/web && bun install --frozen-lockfile)
npm run typecheck
npm run lint
NEXT_PUBLIC_SENTRY_DISABLED=true npm run build
~~~

Go 后端测试和专用测试数据库说明见 [Go 后端迁移说明](docs/go-backend-migration.md)。真实模型与 Amazon 凭据无需用于离线测试。

~~~bash
(cd apps/ecom && go test -race ./... && go vet ./...)
(cd apps/screening && go test -race ./... && go vet ./...)
~~~

## 产品初筛

新增导入、规则初筛、人工批量复核与FBA利润测算，配置与上线步骤见 [产品初筛说明](docs/product-screening.md)，商品主表与月度副表设计见 [数据库方案](docs/product-screening-database.md)，建表与API契约随功能分支交付。已有部署需要先追加初筛数据库密码和内部签名密钥，再更新应用。

## 上游参考

原始源码可查阅 [EcomAgent 上游仓库](https://github.com/yoligehude14753/ecom-agent)或本项目的 [原始导入记录](https://github.com/infMoney-Agent/seller-ops/tree/9374da7/vendor/ecom-agent)。

## 许可证

仓库许可证见 [LICENSE](LICENSE)，引入的 EcomAgent 组件见 [电商后端 MIT 许可证](apps/ecom/LICENSE)及 [前端改编代码许可证](apps/web/src/features/ecom/LICENSE)。

## CI/CD

合入 dev 的检查、镜像发布和服务器自动部署见 [CI/CD 说明](docs/ci-cd.md)。
