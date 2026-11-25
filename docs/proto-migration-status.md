# Proto 迁移进度

目标是继续迁移剩余业务与用户管理元数据接口，不再逐项请求确认。所有批次必须先验证兼容、权限和幂等再切换运行环境；不把 HTTP Handler 反向封装为 RPC，也不使用一个携带任意路径和 JSON 的通用 RPC 冒充契约迁移。

## 已实现的导入模块

- 查询：ListImports、GetImport、ListImportRows、GetJob。
- 操作：GetImportRemovalPreview、CommitImport、UpdateImportSettings、DeleteImport、RetryImportJob。
- 三个独立传输开关：SCREENING_IMPORTS_TRANSPORT、SCREENING_IMPORT_READS_TRANSPORT、SCREENING_IMPORT_WRITES_TRANSPORT。
- 共享 Go 查询和事务用例，保留浏览器旧 JSON、幂等键、200/202 和错误状态。

## 继续迁移的顺序

1. 商品库：列表、统计、详情、父子体、月度记录、来源与历史、商标选项。
2. 分组及人工复核：类目树、工作台、参考样本、单条修改、批次保存及撤销。
3. 利润：商品列表、试算、方案与历史、参数修改、模板和批量任务。
4. 规则及导出元数据：规则版本、试运行、启用、重筛、导出创建。
5. 其余电商模块：选品研究、文案、评论、监控、广告。
6. 用户管理元数据：会话、公司、成员、邀请与加入审批；登录页面、OAuth 回调和 Cookie 设置继续保留标准 HTTP。
7. 观察稳定后按模块移除旧业务路由，保留明确的回滚窗口并核对所有入口。

## 保留 HTTP 的端点

文件上传、原文件/错误报告/导出文件下载继续使用标准 HTTP 流式传输；其创建、元数据和状态查询使用 Proto。健康检查、静态文件、登录页面和 OAuth 回调不纳入普通业务 Unary RPC。

## 验证原则

真实业务数据不用于破坏性测试。使用独立 PostgreSQL 和 Redis 数据库15进行导入、删除及复核验收；运行环境仅核验只读结果和权限。迁移不引入 ORM、不改数据库模式，不改变费用公式或人工复核规则。公共接口路径保持不变。
