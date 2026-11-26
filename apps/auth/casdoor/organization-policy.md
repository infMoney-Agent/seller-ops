# 固定组织标识

Casdoor 组织的 `name` 是 Seller Ops 的身份绑定键。创建后禁止通过后台表单或更新接口修改；所有现有组织（包括 `built-in`）适用。`displayName` 仍可修改，新增、删除接口保持原有权限与约束。

后台新增组织默认生成 `organization_` 加六个随机小写字母，例如 `organization_abcdef`。这是默认值，创建前可编辑；保存后只读且可聚焦、复制。名称冲突继续由服务端拒绝，不覆盖已有组织。

补丁固定用于 Casdoor `v4.25.0`，上游提交 `528a0213f01caf70b9f41009cbf7b2f67bdd67ec`。修改在 `UpdateOrganization` 的任何数据库读取或改名触发器执行前拦截标识变化，避免先改关联用户再报错。

## 构建和部署

```sh
docker build -f apps/auth/casdoor/OrganizationPolicy.Dockerfile \
  -t seller-ops-casdoor:v4.25.0-organization-policy-1 .
```

在服务器私有 Compose 环境文件设置 `CASDOOR_IMAGE=seller-ops-casdoor:v4.25.0-organization-policy-1`，按原有 Compose 配置只重建 `casdoor` 服务。镜像须预先构建或传送到该主机。修改本身不迁移数据库、不修改已有组织或公司绑定。

旧发布版本的 Compose 尚未读取 `CASDOOR_IMAGE` 时，把 `compose.organization-policy.yaml` 复制到仓库外的私有配置目录，并将部署助手的 `SERVER_OVERRIDE` 指向该文件。已有服务器覆盖配置时应合并该服务的 `image` 设置。部署助手会在后续发布中继续使用同一覆盖文件，避免重新切回官方未修改的镜像。

升级时先对新上游版本运行 `git apply --check`，人工复核后更新固定版本和镜像标签。不要将补丁套到版本不明的压缩脚本。回滚时恢复原镜像并仅重建 `casdoor`；无需数据库降级。

## 验证

- Go 回归覆盖普通组织及 `built-in` 的改名、清空、大小写变化拒绝，以及原名保存。
- Node 回归覆盖六个小写字母的格式与随机数边界；在 Node 22+ 执行 `node --experimental-strip-types --test web/tests/organization-identity.test.mjs`。
- 前端执行 TypeScript 检查、Vite 生产构建；后端编译通过后，在独立身份库验证接口拒绝改名、显示名称可保存、新增和删除正常。

本次现网验证：Go 六个场景、Node 两个用例、TypeScript、Vite 与 Go 生产构建通过；独立数据库中的真实接口回归通过；浏览器确认已有组织标识只读，新增默认值为 `organization_xpszdq`。部署后 Casdoor 健康、公开业务登录页均返回 200，已有组织和用户身份记录保持一致。临时测试容器、数据库和隧道已清理。

## Modern Web Guidance

- `forms`：使用原生 `readOnly` 保持组织标识可聚焦、可复制；通过 `htmlFor`/`id` 和 `aria-describedby` 关联标签与持续显示的说明。依据 “Server-side validation is for Security”，服务端同时拒绝改名，避免仅限制浏览器输入。
