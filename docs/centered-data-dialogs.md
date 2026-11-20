# 经营数据与来源数据弹窗

这两层数据查看窗口复用 DataDialogContent 和项目现有 Base UI Dialog，使用视口固定居中定位，宽度上限分别为 56rem 和 42rem，高度为 min(80dvh,48rem)。数据加载前后尺寸不变，不依赖表格按钮位置或碰撞翻转。

标题和关闭按钮在独立、不收缩的头部。只有内容区域滚动，关闭按钮不会随原始行数据滚出窗口。嵌套来源窗口的关闭按钮或 Esc 只关闭当前层，保留外层经营数据和返回焦点。数据接口和加载时机不变。

Modern Web Guidance：dialog。沿用团队成熟的 Base UI Dialog，保留项目已有焦点管理、滚动锁和浏览器兼容处理，遵循项目架构优先规则；未另行手写 focus trap、Esc 监听或实验性 invoker commands。
