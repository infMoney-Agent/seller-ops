# 上架天数与卖家国旗

上架时间下显示距今自然日，今天取 Asia/Shanghai 日历日期，每分钟检查跨日变化。来源日期按独立年月日验证，使用 UTC 数值日序做差，不将日期字符串转换为用户本地时刻，避免夏令时或时区造成一天偏差。无效、缺失及未来上架日期不显示负天数。初始 SSR 与客户端都不输出未确定的天数，挂载后更新。

卖家国旗使用 country-flag-icons 1.6.20 的 MIT SVG 静态资源，授权见 public/images/flags/LICENSE，无新增运行时依赖或外部图片请求。国旗作为装饰，所属地原文字仍显示；识别ISO代码、中英文地区名及UK等常用别名，未知地区保留原文。

Modern Web Guidance：capture-location-agnostic-data。遵循日期与时刻分离、显式时区的原则；为兼容现有浏览器与项目避免简单字段显示引入 Temporal polyfill，使用验证后的数字年月日计算自然日作为兼容实现。
