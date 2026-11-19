# 前端部署

Seller Ops 的入口由 Next.js 提供，并通过内部网络连接 Go 认证服务和 FastAPI 业务后端。完整部署步骤见 [项目部署说明](../../../docs/deployment.md)。

独立构建前端镜像时，从仓库根目录执行：

~~~bash
docker build -f apps/web/Web.Dockerfile -t seller-ops-web .
~~~

运行前端时设置 SELLER_API_URL、SELLER_APP_ORIGIN 和 ECOM_API_URL，具体地址见 [项目 README](../../../README.md)。
