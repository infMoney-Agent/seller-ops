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

## 启动命令

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

## 环境变量

复制 `apps/web/env.example.txt` 为 `apps/web/.env.local` 并填写所需配置（Clerk 认证、Sentry 等）。
