# 登录页按需加载

补丁 progressive-login.patch 应用于原生部署使用的 Casdoor 前端源码，不改变 OAuth、PKCE、验证码或权限规则。

- 普通登录不导入管理布局，进入管理路由后才加载 AppLayout。
- 人脸识别组件只在触发人脸验证后加载；局部 Suspense 提示不会隐藏账号密码表单。
- 原有 React.lazy 后台页面继续保留；登录所需翻译和必需验证仍正常加载。

升级 Casdoor 时先在源码根目录执行 git apply --check，再应用补丁并运行前端类型检查与 Vite 构建。发布时先复制新 assets，最后原子替换 index.html；保留旧 assets 以兼容尚未刷新的页面。不要直接运行会删除旧构建的 mv.js。

检查实际登录入口及 LoginPage 的静态依赖闭包；不把不同认证方式的动态依赖算进普通密码登录首屏。当前对比：JS 约 1.85MB → 1.17MB，gzip 约 542KB → 380KB。此数据不等同于整页加载时间。

回滚时恢复上一份 index.html；带哈希的静态文件可以保留，避免使已打开页面失效。

## Modern Web Guidance

- performance：通过 import() 延迟实际未使用的模块，保留登录表单状态及原有认证限制。

## 同域电商回调修复

同一网关下还需应用 same-origin-callback.patch：只有 /callback、/callback/* 和 /api/callback 属于 Casdoor 自身回调，/api/auth/callback 必须按客户端 OAuth 授权码流程完成。否则客户端拿不到授权码，用户会误入账号管理 SPA。补丁附带 Node 回归用例；使用 Node 22 执行 node --experimental-strip-types --test web/tests/self-redirect.test.mjs。
