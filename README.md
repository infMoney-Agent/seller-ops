# seller-ops

基于 Next.js 16 + shadcn/ui + TypeScript 的管理后台项目，采用 monorepo 结构。

## 目录结构

```
seller-ops/
├── apps/
│   └── web/          # Next.js 应用（源码、配置、依赖均在此目录）
├── docs/             # 项目文档
└── package.json      # 根 package.json（脚本代理到 apps/web）
```

## 直接在 Ubuntu 上启动进程

这套方式用于经 SSH 转发的本地测试。准备 Node.js 和 Web 依赖、Go（版本见 `apps/server/go.mod`）、PostgreSQL，以及带前端资源的 Casdoor。Casdoor 可按官方说明运行。业务数据库仍使用 PostgreSQL。 [Casdoor 安装说明](https://casdoor.ai/docs/basic/server-installation/)

1. 启动 PostgreSQL，创建一个供 Go 使用的空数据库 `seller_ops`，准备该账号的连接串。Casdoor 可使用单独的 SQLite 数据库。在 Casdoor 的 `conf/app.conf` 中设置以下值，从 Casdoor 安装目录运行 `./casdoor`。

   ```ini
   httpaddr = 127.0.0.1
   httpport = 18444
   origin = http://localhost:18444
   runmode = prod
   driverName = sqlite
   dataSourceName = file:casdoor.db?cache=shared
   dbName = casdoor
   ```

2. 在 Windows PowerShell 保持 SSH 转发开启。

   ```powershell
   ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:18443:127.0.0.1:18443 -L 127.0.0.1:18444:127.0.0.1:18444 yijiangli@43.139.227.179
   ```

   用 Edge 或 Chrome 打开 [Casdoor](http://localhost:18444)，按下文「首次配置 Casdoor」设置账号和 OIDC 应用。回调地址为 `http://localhost:18443/api/auth/callback`。在 Ubuntu 上可用 `curl -fsS http://localhost:18444/.well-known/openid-configuration` 确认 Casdoor 的 issuer 为 `http://localhost:18444`。

3. 在 Ubuntu 的一个终端启动 Go 服务。以下示例连接本机 PostgreSQL，请按实际账号、端口和密码填写连接串。

   ```bash
   cd /data/workspace/yijiangli/seller-ops/apps/server
   export SELLER_LISTEN_ADDR=127.0.0.1:8080
   export APP_ORIGIN=http://localhost:18443
   export OIDC_ISSUER=http://localhost:18444
   read -r -s -p 'DATABASE_URL: ' DATABASE_URL
   printf '\n'
   read -r -p 'OIDC_CLIENT_ID: ' OIDC_CLIENT_ID
   read -r -s -p 'OIDC_CLIENT_SECRET: ' OIDC_CLIENT_SECRET
   printf '\n'
   export DATABASE_URL OIDC_CLIENT_ID OIDC_CLIENT_SECRET
   go run .
   ```

4. 在另一个 Ubuntu 终端启动 Next.js，并用 Windows 浏览器打开 [seller-ops](http://localhost:18443)。

   ```bash
   cd /data/workspace/yijiangli/seller-ops/apps/web
   SELLER_API_URL=http://127.0.0.1:8080 npm run dev -- --hostname 127.0.0.1 --port 18443
   ```

   测试账号登录一次后，在另一个已配置同一 `DATABASE_URL` 的 Ubuntu 终端进入 `apps/server`，运行 `go run . create-workspace "Test A" owner@example.com`。把示例邮箱换成测试账号的已验证邮箱。刷新浏览器即可选择工作区。

## 无公网域名时启动整套应用

以下配置用于个人测试。服务器需要 Docker Engine 和 Docker Compose 2.24.4 或更新版本，SSH 账号需要能运行 `docker compose`。应用和 Casdoor 通过 SSH 转发访问，服务器只发布回环端口。

### 1. 在 Ubuntu 上准备配置

在项目根目录执行。以下命令首次创建配置文件，之后请直接编辑已有文件。

```bash
cd /data/workspace/yijiangli/seller-ops
mkdir -p -m 700 "$HOME/.config/seller-ops-local"
local_dir="$HOME/.config/seller-ops-local"

if [ ! -e "$local_dir/compose.env" ]; then
  (
    umask 077
    cat > "$local_dir/compose.env" <<EOF
APP_DOMAIN=localhost:18443
AUTH_DOMAIN=localhost:18444
POSTGRES_PASSWORD=$(openssl rand -hex 32)
APP_DB_PASSWORD=$(openssl rand -hex 32)
AUTH_DB_PASSWORD=$(openssl rand -hex 32)
OIDC_CLIENT_ID=
OIDC_CLIENT_SECRET=
EOF
  )
fi

seller_local() {
  docker compose --env-file "$HOME/.config/seller-ops-local/compose.env" \
    -p seller-local -f compose.casdoor.yaml -f compose.local.yaml "$@"
}

seller_local up -d db casdoor proxy
```

重新登录 Ubuntu 时，需要在项目根目录重新定义 `seller_local` 函数。

### 2. 在 Windows 上建立 SSH 转发

在 PowerShell 窗口保持转发开启。请用 Edge 或 Chrome 访问下方的 `localhost` 地址，浏览器可以在该地址使用应用的 Secure Cookie。

```powershell
ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:18443:127.0.0.1:18443 -L 127.0.0.1:18444:127.0.0.1:18444 yijiangli@43.139.227.179
```

### 3. 首次配置 Casdoor

用 Edge 或 Chrome 打开 [Casdoor 管理界面](http://localhost:18444)。修改初始管理员密码，创建身份组织、OIDC 应用和邮箱已验证的测试账号。关闭公开注册，回调地址填 `http://localhost:18443/api/auth/callback`。应用使用签名 ID Token，并提供 `iss`、`sub`、`nonce`、`email` 和 `email_verified` 声明。

将客户端 ID 和密钥填入服务器上的 `$HOME/.config/seller-ops-local/compose.env`。

### 4. 启动应用并创建工作区

回到 Ubuntu 项目目录执行。

```bash
seller_local --profile app up -d --build
seller_local --profile app ps
```

打开 [seller-ops](http://localhost:18443)，用测试账号登录一次。然后将下方邮箱换成该账号的邮箱，创建首个工作区。

```bash
seller_local --profile app exec api seller-server create-workspace "Test A" owner@example.com
```

刷新页面后可选择工作区。排查启动问题可用 `seller_local --profile app logs --tail=100 api web proxy casdoor`。停止测试环境可用 `seller_local --profile app down`，数据库卷会保留。

## 仅运行前端开发服务器

### 方式一：在根目录执行（推荐）

```bash
npm install          # 安装依赖
npm run dev          # 启动开发服务器，访问 http://localhost:3000
npm run build        # 构建生产版本
npm run start        # 启动生产服务器
```

### 方式二：在 apps/web 目录执行

```bash
cd apps/web
npm install
npm run dev
```

## 其他命令

```bash
npm run typecheck    # TypeScript 类型检查
npm run lint         # 代码检查
npm run lint:fix     # 代码检查并修复
npm run format       # 格式化代码
npm run format:check # 检查代码格式
```

## 认证与部署

登录由自托管 Casdoor 提供。Go 服务负责工作区权限和产品接口，PostgreSQL 持久化会话、成员与产品。Next.js 通过内部地址 `SELLER_API_URL` 连接 Go 服务。完整配置与测试步骤见 [Casdoor 部署说明](deploy/casdoor/README.md)。

## E-commerce workspace

Seller Ops opens the e-commerce dashboard after Casdoor sign-in. Product
Research, Listing Generator, Review Analyzer, Competitor Monitor, and Ad
Optimizer are available in the sidebar. Account, profile, and workspace
management use the existing Seller Ops pages.

The existing Casdoor deployment with the Compose app profile starts the
e-commerce API, Redis, and scheduled monitor worker as well as the web app.
The browser uses Seller Ops' authenticated API routes. The FastAPI and Redis
services use the Compose network without published ports. Monitors and their
snapshots are stored by Seller Ops account ID.

For live operations, provide credentials in the deployment environment for
your selected LLM provider and any Amazon APIs you use. The starting variable
names are in vendor/ecom-agent/.env.example. Playwright Chromium is installed
by the API image. Provider credentials are shared by the deployment.

For a native server setup, use SELLER_API_URL for the existing Go authentication
service, SELLER_APP_ORIGIN for the browser-facing origin registered in Casdoor,
and ECOM_API_URL for the private FastAPI URL. Bind the FastAPI and Redis
services to loopback and configure Redis persistence. Run the Celery worker and
beat scheduler from vendor/ecom-agent/backend for hourly monitor snapshots.
