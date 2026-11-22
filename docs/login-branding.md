# 登录入口与品牌

/auth/sign-in 服务端直接跳转 /api/auth/login，由现有Go认证服务生成state、nonce、PKCE与Cookie，再进入身份登录表单。仅带error参数时显示重试提示，避免错误流程反复跳转。中间确认页面及未使用的组件已移除。

login-branding.json 仅定义应用UI，Logo与favicon由Web本地静态文件提供。保留身份提供方、客户端、用户组织、登录方法、注册及密码策略、回调地址和OIDC验证不变。

当前本地Casdoor使用SQLite，可通过 brand-local-login.mjs 更新与 OIDC_CLIENT_ID 唯一匹配的应用；必须显式提供 CASDOOR_SQLITE_PATH、APP_ORIGIN 和 CASDOOR_BRANDING_BACKUP。仅备份和更新品牌字段，备份权限0600。持久化数据库不会重新执行初始化，已有运行环境需显式应用配置。其他数据库部署通过身份控制台应用相同品牌字段，不使用此SQLite工具。

回滚恢复对应应用品牌备份中的字段；Web可回滚至上版镜像。无需删除用户、会话或认证数据库。
