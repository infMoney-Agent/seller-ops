"""Pure domain rules. No database, network, model calls or implicit fee defaults."""

import csv
import hashlib
import io
import json
import re
import zipfile
from decimal import Decimal, InvalidOperation, ROUND_HALF_UP, localcontext
from pathlib import Path
import regex

MAX_BYTES = 30 * 1024 * 1024
MAX_EXPANDED = 250 * 1024 * 1024
MAX_ROWS = 100_000
FORMULA_VERSION = "fba-us-v1"
ALIASES = {
    "asin": ["ASIN", "asin"],
    "title": ["商品标题", "Title", "title"],
    "title_zh": ["商品标题中文（机器翻译）", "中文标题", "商品标题中文"],
    "image_url": ["商品主图", "主图", "Image URL"],
    "category": ["小类目", "Category"],
    "brand": ["品牌", "Brand"],
    "seller_id": ["卖家ID", "Seller ID", "SellerId"],
    "seller_name": ["卖家名称", "BuyBox卖家", "Seller Name"],
    "seller_region": ["卖家所属地", "卖家地区", "Seller Region"],
    "price_usd": ["价格($)", "Price", "price"],
    "monthly_sales": ["月销量", "Monthly Sales"],
    "monthly_revenue": ["月销售额($)", "月销售额", "Monthly Revenue"],
    "bsr_rank": ["BSR排名", "BSR Rank", "BSR"],
    "review_count": ["评分数", "Review Count"],
    "rating": ["评分", "Rating"],
}


def digest(value):
    return hashlib.sha256(
        json.dumps(value, ensure_ascii=False, sort_keys=True, default=str).encode()
    ).hexdigest()


def numeric(value):
    if value is None or value == "":
        return None
    try:
        n = Decimal(str(value).replace(",", "").replace("$", "").strip())
        return str(n) if n.is_finite() else None
    except InvalidOperation:
        return None


def normalize(raw, mapping):
    out, errors = {}, []
    for field, aliases in ALIASES.items():
        header = mapping.get(field)
        value = (
            raw.get(header)
            if header
            else next((raw[k] for k in aliases if raw.get(k) not in (None, "")), None)
        )
        if field in (
            "price_usd",
            "monthly_sales",
            "monthly_revenue",
            "review_count",
            "rating",
            "bsr_rank",
        ):
            value = numeric(value)
            if value is not None:
                n = Decimal(value)
                if (
                    n < 0
                    or (field == "rating" and n > 5)
                    or (
                        field in ("monthly_sales", "review_count", "bsr_rank")
                        and n != n.to_integral_value()
                    )
                    or n > Decimal("999999999999")
                ):
                    errors.append(f"{field} 数值无效")
                if field in ("monthly_sales", "review_count", "bsr_rank"):
                    value = int(n)
        else:
            value = str(value).strip() if value is not None else None
        out[field] = value
    out["asin"] = (out["asin"] or "").upper()
    if not re.fullmatch(r"[A-Z0-9]{10}", out["asin"]):
        errors.append("ASIN 必须为10位字母数字")
    if not out["title"]:
        errors.append("缺少商品标题")
    url = out.get("image_url") or ""
    if url and not url.startswith("https://"):
        out["image_url"] = None
    out["specs"] = {"详细参数": str(raw.get("详细参数") or "")}
    return out, errors


def _archive(data):
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        entries = [i for i in archive.infolist() if not i.is_dir()]
        if len(entries) > 100 or sum(i.file_size for i in entries) > MAX_EXPANDED:
            raise ValueError("ZIP 文件数量或展开体积超限")
        for item in entries:
            path = Path(item.filename)
            if path.is_absolute() or ".." in path.parts or item.flag_bits & 1:
                raise ValueError("ZIP 路径或加密文件不受支持")
            if path.suffix.lower() == ".zip":
                raise ValueError("禁止嵌套 ZIP")
            if path.suffix.lower() in (".xlsx", ".csv") and not path.name.startswith(
                "~$"
            ):
                yield item.filename, archive.read(item)


def parse_file(data, name, mapping):
    if len(data) > MAX_BYTES:
        raise ValueError("文件超过30MB")
    members = _archive(data) if name.lower().endswith(".zip") else [(name, data)]
    expanded = 0
    for member, content in members:
        try:
            suffix = Path(member).suffix.lower()
            if suffix == ".xlsx":
                with zipfile.ZipFile(io.BytesIO(content)) as package:
                    expanded += sum(i.file_size for i in package.infolist())
                if expanded > MAX_EXPANDED:
                    raise ValueError("Excel 展开体积超过250MB")
                from openpyxl import load_workbook

                book = load_workbook(
                    io.BytesIO(content), read_only=True, data_only=True
                )
                try:
                    sheets = [
                        (sheet.title, sheet.iter_rows(values_only=True))
                        for sheet in book.worksheets
                    ]
                    yield from _parse_sheets(sheets, member, mapping)
                finally:
                    book.close()
            elif suffix == ".csv":
                expanded += len(content)
                if expanded > MAX_EXPANDED:
                    raise ValueError("文件展开体积超过250MB")
                try:
                    decoded = content.decode("utf-8-sig")
                except UnicodeDecodeError:
                    decoded = content.decode("gb18030")
                yield from _parse_sheets(
                    [("", csv.reader(io.StringIO(decoded)))], member, mapping
                )
            else:
                raise ValueError("仅支持 XLSX、CSV、ZIP")
        except Exception as error:
            yield {
                "member_path": member,
                "sheet_name": "",
                "row_no": 1,
                "raw_data": {},
                "normalized_data": None,
                "errors": [str(error)],
            }


def _parse_sheets(sheets, member, mapping):
    found = False
    for sheet_name, iterator in sheets:
        header = None
        for index, values in enumerate(iterator, 1):
            if header is None:
                keys = [str(v).strip() if v is not None else "" for v in values]
                if (mapping.get("asin") or "ASIN") in keys or "asin" in keys:
                    if len([k for k in keys if k]) != len(set(k for k in keys if k)):
                        raise ValueError(f"{sheet_name} 表头含重名列")
                    header = keys
                    found = True
                elif index >= 20:
                    break
                continue
            if not any(v not in (None, "") for v in values):
                continue
            raw = {k: v for k, v in zip(header, values) if k}
            normalized, errors = normalize(raw, mapping)
            yield {
                "member_path": member,
                "sheet_name": sheet_name,
                "row_no": index,
                "raw_data": raw,
                "normalized_data": normalized,
                "errors": errors,
            }
    if not found:
        raise ValueError("前20行未识别 ASIN 表头，请调整字段映射")


def validate_policy(config):
    if not isinstance(config, dict):
        raise ValueError("规则必须为对象")
    if len(json.dumps(config, ensure_ascii=False)) > 128 * 1024:
        raise ValueError("规则配置超过128KB")
    if not isinstance(config.get("exclude", {}), dict) or any(
        not isinstance(v, bool) for v in config.get("exclude", {}).values()
    ):
        raise ValueError("排除开关必须为布尔值")
    if (
        not isinstance(config.get("minimum_occurrences", 1), int)
        or not 1 <= config.get("minimum_occurrences", 1) <= 120
    ):
        raise ValueError("来源期数必须在1至120之间")
    price = config.get("price", {})
    if not isinstance(price, dict):
        raise ValueError("价格规则必须为对象")
    for key in ("min", "max"):
        if price.get(key) is not None and numeric(price[key]) is None:
            raise ValueError("价格阈值无效")
    for name in ("overrides", "exploration_recommendations"):
        if config.get(name):
            raise ValueError("公共规则禁止带入历史ASIN覆盖项")
    patterns = list(_patterns(config))
    if len(patterns) > 300:
        raise ValueError("最多300条匹配规则")
    for label, pattern in patterns:
        if not isinstance(pattern, str) or len(pattern) > 2000:
            raise ValueError("表达式长度超限")
        try:
            regex.compile(pattern, regex.I)
        except regex.error as error:
            raise ValueError(f"表达式无效：{label}") from error
    return config


def _patterns(cfg):
    yield from cfg.get("exclusion_patterns", {}).items()
    yield from cfg.get("extra_exclusions", [])
    yield from cfg.get("mandatory_exclusions", [])
    for rule in cfg.get("grouping", {}).get("custom_rules", []):
        yield rule["group"], rule["pattern"]


def match(pattern, value):
    return bool(regex.search(pattern, value, regex.I, timeout=0.05))


def classify(product, cfg):
    title = product.get("title") or ""
    text = title + " " + (product.get("category") or "")
    reasons, evidence = [], []
    if not title:
        return {
            "decision": "insufficient",
            "reasons": ["缺少标题"],
            "direction": "信息不足",
            "recommendation": "regular",
            "evidence": [],
        }
    check = re.sub(
        r"\b(?:non[- ]electric|no electric|without electric|no led)\b",
        "manual",
        text,
        flags=re.I,
    )
    is_bag = bool(re.search(r"(vacuum|compression).*bag|space saver bags", text, re.I))
    pump_unknown = (
        is_bag
        and bool(re.search(r"\bpump\b", check, re.I))
        and not re.search(
            r"hand pump|manual pump|without.*pump|no pump|electric|battery|rechargeable",
            check,
            re.I,
        )
    )
    container = bool(
        re.search(r"\b(dispenser|holder|dish|rack|container|bottle)\b", title, re.I)
    ) and not re.search(r"filled|with soap|with solution|included.*soap", title, re.I)
    passive = bool(
        re.search(
            r"replacement.*(?:heads?|bags?|filters?)|(?:heads?|bags?|filters?).*replacement",
            title,
            re.I,
        )
    ) and not re.search(r"with.*(?:motor|battery)|rechargeable", title, re.I)
    try:
        for label, pattern in cfg.get("mandatory_exclusions", []):
            if match(pattern, title):
                reasons.append(label)
                evidence.append({"rule": label, "kind": "mandatory"})
        mandatory = bool(reasons)
        for label, pattern in cfg.get("exclusion_patterns", {}).items():
            if cfg.get("exclude", {}).get(label, True) and match(pattern, check):
                if label == "电器或电池" and passive:
                    continue
                if label == "化学制剂或含配方套装" and container:
                    continue
                reasons.append(label)
                evidence.append({"rule": label, "kind": "pattern"})
        for label, pattern in cfg.get("extra_exclusions", []):
            if match(pattern, check):
                reasons.append(label)
                evidence.append({"rule": label, "kind": "extra"})
        if cfg.get("consumable_only") and not is_bag:
            if not match(cfg.get("consumable_pattern", r"(?!)"), text):
                reasons.append("消耗属性不明确")
            if product.get("category") in cfg.get("durable_categories", []) or match(
                cfg.get("durable_title_pattern", r"(?!)"), title
            ):
                reasons.append("耐用品或耗材收纳器")
        if product.get("occurrences", 1) < cfg.get("minimum_occurrences", 1):
            reasons.append("来源期数低于配置")
        direction = product.get("category") or "未校准方向：" + product["asin"]
        for rule in cfg.get("grouping", {}).get("custom_rules", []):
            if match(rule["pattern"], text):
                direction = rule["group"]
                break
        if not cfg.get("price", {}).get("use_source_filter", True):
            price = numeric(product.get("price_usd"))
            if price is None:
                return {
                    "decision": "insufficient",
                    "reasons": ["价格缺失"],
                    "direction": direction,
                    "recommendation": "regular",
                    "evidence": evidence,
                }
            for key, op in [("min", lambda a, b: a < b), ("max", lambda a, b: a > b)]:
                limit = cfg["price"].get(key)
                if limit is not None and op(Decimal(price), Decimal(str(limit))):
                    reasons.append("价格超配置")
        from .grouping_rules import grouping_details
        from .direction_rules import direction as base_direction

        raw = {
            "ASIN": product["asin"],
            "商品标题": title,
            "小类目": product.get("category") or "",
            "品牌": product.get("brand") or "",
            "卖家ID": product.get("seller_id") or "",
            **product.get("specs", {}),
        }
        base = base_direction([None, None, title, raw["小类目"]])
        details = grouping_details(raw, base or direction)
        direction = details["direction"]
        evidence.append({"rule": "用途材质结构归并", "basis": details["basis"]})
        decision = "excluded" if reasons else "passed"
        if pump_unknown and not mandatory:
            decision = "insufficient"
            reasons.append("真空袋泵类型不明")
        exploration = bool(reasons and not mandatory and (container or passive))
        return {
            "decision": decision,
            "reasons": list(dict.fromkeys(reasons)) or ["符合当前规则"],
            "direction": direction,
            "recommendation": "exploration" if exploration else "regular",
            "evidence": evidence,
        }
    except TimeoutError:
        return {
            "decision": "insufficient",
            "reasons": ["规则执行超时，请检查表达式"],
            "direction": product.get("category") or product["asin"],
            "recommendation": "regular",
            "evidence": [],
        }


PROFIT_FIELDS = [
    "sale_price_usd",
    "discount_usd",
    "purchase_cny",
    "packaging_cny",
    "freight_cny",
    "other_landed_cny",
    "fx_cny_per_usd",
    "commission_rate",
    "fba_fee_usd",
    "storage_usd",
    "ad_rate",
    "return_rate",
    "loss_per_return_usd",
    "other_usd",
    "monthly_units",
]


def calculate(inputs):
    missing = [key for key in PROFIT_FIELDS if inputs.get(key) in (None, "")]
    if missing:
        return {
            "state": "incomplete",
            "missing": missing,
            "formula_version": FORMULA_VERSION,
        }
    try:
        values = {key: Decimal(str(inputs[key])) for key in PROFIT_FIELDS}
    except (InvalidOperation, ValueError):
        raise ValueError("费用必须为有效十进制数值")
    for key, value in values.items():
        if not value.is_finite() or value < 0 or value > Decimal("999999999999"):
            raise ValueError(f"{key} 数值无效")
        if key.endswith("_rate") and value > 1:
            raise ValueError(f"{key} 必须在0到1之间")
    if (
        values["sale_price_usd"] <= values["discount_usd"]
        or values["fx_cny_per_usd"] <= 0
    ):
        raise ValueError("净售价和汇率必须大于零")
    if values["monthly_units"] != values["monthly_units"].to_integral_value():
        raise ValueError("月销量必须为整数")
    with localcontext() as ctx:
        ctx.prec = 40
        p = values["sale_price_usd"] - values["discount_usd"]
        c = (
            sum(
                values[k]
                for k in (
                    "purchase_cny",
                    "packaging_cny",
                    "freight_cny",
                    "other_landed_cny",
                )
            )
            / values["fx_cny_per_usd"]
        )
        returns = values["return_rate"] * values["loss_per_return_usd"]
        fixed = (
            c
            + values["fba_fee_usd"]
            + values["storage_usd"]
            + values["other_usd"]
            + returns
        )
        denominator = 1 - values["commission_rate"] - values["ad_rate"]
        profit = p * denominator - fixed
        results = {
            "net_price_usd": p,
            "landed_cost_usd": c,
            "return_allowance_usd": returns,
            "profit_usd": profit,
            "margin": profit / p,
            "roi": profit / c if c else None,
            "monthly_profit_usd": profit * values["monthly_units"],
            "break_even_price_usd": fixed / denominator if denominator > 0 else None,
            "break_even_ad_rate": 1 - values["commission_rate"] - fixed / p,
        }
        return {
            "state": "complete",
            "formula_version": FORMULA_VERSION,
            **{
                k: format(
                    v.quantize(
                        Decimal("0.000001")
                        if k in ("margin", "roi", "break_even_ad_rate")
                        else Decimal("0.01"),
                        rounding=ROUND_HALF_UP,
                    ),
                    "f",
                )
                if v is not None
                else None
                for k, v in results.items()
            },
        }


def excel_text(value):
    value = str(value or "")
    return "'" + value if value.lstrip().startswith(("=", "+", "-", "@")) else value
