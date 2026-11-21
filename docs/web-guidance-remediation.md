# 前端指南检查整改

本轮落实 2026-11-19 检查中的八项问题，不修改业务数据库、接口身份验证、人工结论和利润公式。

| 问题 | 实现与采用的指南 |
| --- | --- |
| 监控快照缓存被反转 | 使用 toReversed 保持缓存不可变；js："without mutating the source reference"。 |
| 中文输入确认误提交 | chat 和 TagsField 在处理 Enter 前检查 nativeEvent.isComposing / keyCode 229；ime-safe-enter-submit："Add a keyCode === 229 check"，保留 Safari 兼容保护和 Shift+Enter。 |
| 监控卡片键盘操作 | 原生商品选择按钮与记录、删除按钮并列，不嵌套按钮；命名按钮并暴露 aria-pressed；accessibility。 |
| 利润输入未校验 | 统一 form submit 路径，校验全部待比较方案，切换到错误方案并聚焦首个字段；forms："move focus to the first invalid field"。保留空成本和批量空售价，仍由 API 返回待补充；分母异常等公式边界不擅自禁止。 |
| 标签未关联 | 各电商页面的可见 label 与唯一 input/select/textarea ID 对应，并用 aria-labelledby 保持名称一致；forms/accessibility。 |
| 复制失败无提示 | 复制成功与失败均用可见 role=status 反馈，清理提示计时器，重复点击重置并处理重复播报；copy-button："show the failure too"。沿用 React 点击处理，不引入实验性 Invoker Commands 或弃用的 execCommand 回退。 |
| 导入完成仍轮询 | 列表仅在有运行批次时轮询并退避；预览只在切换页、任务完成和状态变化时刷新。 |
| 滚动表格缺少键盘入口 | 复用具名、可聚焦的 TableScroll，补 caption 与 scope=col；responsive-table："tabindex=\"0\""。对命名溢出区域记录必要的局部 lint 例外；不强制将复杂数据表改为卡片。 |

## 验证

- npm run test:web-guidance：利润范围、零值、负数、正汇率、整数销量和缺失成本边界。
- 相关 Web 类型检查和锁定版本 oxlint。
- 现有隔离 Chrome 验收增加利润全部方案校验/焦点、终态停止轮询、监控键盘选择与稳定顺序、标签关联、复制拒绝/成功、组合输入事件及窄屏键盘滚动。
- 这些浏览器检查使用专用测试库和浏览器内合成接口，不触及真实商品。
- Chrome 中模拟组合输入和剪贴板异常不等于真实 Safari、输入法和读屏器验收；其他浏览器实测仍有待补充。
