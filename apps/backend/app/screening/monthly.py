"""Product identity is durable; prices and sales belong to explicit observation periods."""

import calendar
import re
from datetime import date, timedelta
from decimal import Decimal
from uuid import UUID, uuid4
from .db import execute, one

METRIC_FIELDS = (
    "price_usd",
    "monthly_sales",
    "monthly_revenue",
    "review_count",
    "rating",
    "bsr_rank",
)
METRIC_SELECT = """m.id AS monthly_id,m.revision AS monthly_revision,m.stat_month,m.period_type,m.period_start,m.period_end,m.observed_on AS monthly_observed_on,m.source_type,m.source_row_id AS monthly_source_row_id,m.currency,m.price_usd::text AS price_usd,m.monthly_sales,m.monthly_revenue::text AS monthly_revenue,m.review_count,m.rating::text AS rating,m.bsr_rank"""
CURRENT_JOIN = " LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id "


def period_metadata(
    observed_on,
    source_period="",
    period_type="calendar_month",
    period_start=None,
    period_end=None,
):
    if period_type not in ("calendar_month", "rolling_30d", "custom"):
        raise ValueError("统计口径必须为自然月、滚动30天或自定义")

    def parse(value):
        return (
            date.fromisoformat(value)
            if isinstance(value, str) and value
            else value or None
        )

    observed_on, start, end = parse(observed_on), parse(period_start), parse(period_end)
    if (start is None) != (end is None):
        raise ValueError("统计起止日期需要一起填写")
    if start is None and observed_on is not None:
        if period_type == "calendar_month":
            if re.match(r"^\d{4}-", source_period or "") and not re.fullmatch(
                r"[12]\d{3}-(0[1-9]|1[0-2])", source_period or ""
            ):
                raise ValueError("自然月来源期应为有效的 YYYY-MM")
            month = (
                source_period
                if re.fullmatch(r"[12]\d{3}-(0[1-9]|1[0-2])", source_period or "")
                else observed_on.strftime("%Y-%m")
            )
            start = date.fromisoformat(month + "-01")
            end = start.replace(day=calendar.monthrange(start.year, start.month)[1])
        elif period_type == "rolling_30d":
            end = observed_on
            start = end - timedelta(days=29)
        else:
            raise ValueError("自定义统计口径必须填写起止日期")
    if start is not None:
        if start > end:
            raise ValueError("统计开始日期不能晚于结束日期")
        if period_type == "calendar_month" and (
            start.day != 1
            or start.year != end.year
            or start.month != end.month
            or end.day != calendar.monthrange(end.year, end.month)[1]
        ):
            raise ValueError("自然月必须覆盖完整月份，请改用自定义口径")
        if (
            period_type == "calendar_month"
            and re.fullmatch(r"[12]\d{3}-(0[1-9]|1[0-2])", source_period or "")
            and start.strftime("%Y-%m") != source_period
        ):
            raise ValueError("自然月起止日期与来源月份不一致")
        if period_type == "rolling_30d" and (end - start).days != 29:
            raise ValueError("滚动周期必须为30天")
    return {"period_type": period_type, "period_start": start, "period_end": end}


def base_for(base, params, product_ids=None):
    month = params.get("stat_month")
    source = params.get("source_type")
    kind = params.get("period_type") or ("calendar_month" if month else None)
    if not month and not source and not kind:
        return base
    clauses = ["mm.tenant_id=:t"]
    if month:
        try:
            month = date.fromisoformat(
                str(month) + "-01" if len(str(month)) == 7 else str(month)
            )
        except ValueError as error:
            raise ValueError("统计月份应为 YYYY-MM") from error
        if month.day != 1:
            raise ValueError("统计月份必须为当月第一天")
        clauses.append(f"mm.stat_month=DATE '{month.isoformat()}'")
    if kind:
        if kind not in ("calendar_month", "rolling_30d", "custom"):
            raise ValueError("统计口径无效")
        clauses.append(f"mm.period_type='{kind}'")
    if source:
        if not re.fullmatch(r"[a-zA-Z0-9_-]{1,50}", str(source)):
            raise ValueError("数据来源无效")
        clauses.append(f"mm.source_type='{source}'")
    if product_ids is not None:
        identities = [str(UUID(str(value))) for value in product_ids]
        clauses.append(
            "mm.product_id IN ("
            + ",".join("'" + value + "'::uuid" for value in identities)
            + ")"
            if identities
            else "false"
        )
    # Values are parsed/whitelisted above; no client-provided SQL is interpolated.
    join = f""" LEFT JOIN (SELECT DISTINCT ON (mm.tenant_id,mm.product_id) mm.* FROM ps_product_monthly mm WHERE {" AND ".join(clauses)} ORDER BY mm.tenant_id,mm.product_id,mm.stat_month DESC,mm.period_end DESC,mm.observed_on DESC,mm.updated_at DESC,mm.id DESC) m ON m.tenant_id=p.tenant_id AND m.product_id=p.id """
    return base.replace(CURRENT_JOIN, join, 1)


async def product_with_metrics(conn, tenant, id, monthly_id=None):
    join = CURRENT_JOIN
    args = {"t": tenant, "id": str(id)}
    if monthly_id is not None:
        join = " LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.product_id=p.id AND m.id=:mid "
        args["mid"] = str(monthly_id)
    return await one(
        conn,
        "SELECT p.*,"
        + METRIC_SELECT
        + " FROM ps_products p"
        + join
        + " WHERE p.tenant_id=:t AND p.id=:id",
        **args,
    )


def row_order(import_record, row):
    return (
        import_record["observed_on"],
        import_record["created_at"],
        row["member_path"],
        row["sheet_name"],
        row["row_no"],
        str(row["id"]),
    )


async def upsert_monthly(conn, tenant, product_id, import_record, row):
    bounds = period_metadata(
        import_record["observed_on"],
        import_record["source_period"],
        import_record["period_type"],
        import_record["period_start"],
        import_record["period_end"],
    )
    start, end = bounds["period_start"], bounds["period_end"]
    if not start or not end:
        raise ValueError("确认导入前必须填写观察日期和统计周期")
    old = await one(
        conn,
        """SELECT m.*,r.member_path,r.sheet_name,r.row_no,r.id AS row_id,i.created_at AS import_created FROM ps_product_monthly m JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE m.tenant_id=:t AND m.product_id=:p AND m.source_type=:source AND m.period_type=:kind AND m.period_start=:start AND m.period_end=:end""",
        t=tenant,
        p=product_id,
        source=import_record["source_type"],
        kind=bounds["period_type"],
        start=start,
        end=end,
    )
    if old:
        previous = (
            old["observed_on"],
            old["import_created"],
            old["member_path"],
            old["sheet_name"],
            old["row_no"],
            str(old["row_id"]),
        )
        if row_order(import_record, row) <= previous:
            return old["id"]
    normalized = row["normalized_data"]
    values = {key: normalized.get(key) for key in METRIC_FIELDS}
    for key in ("price_usd", "monthly_revenue", "rating"):
        if values[key] is not None:
            values[key] = Decimal(str(values[key]))
    args = {
        **values,
        "t": tenant,
        "p": product_id,
        "id": str(old["id"]) if old else str(uuid4()),
        "month": end.replace(day=1),
        "kind": bounds["period_type"],
        "start": start,
        "end": end,
        "observed": import_record["observed_on"],
        "source": import_record["source_type"],
        "row": str(row["id"]),
    }
    if old:
        await execute(
            conn,
            "UPDATE ps_product_monthly SET "
            + ",".join(f"{key}=:{key}" for key in METRIC_FIELDS)
            + ",source_row_id=:row,observed_on=:observed,revision=revision+1,updated_at=now() WHERE tenant_id=:t AND id=:id",
            **args,
        )
    else:
        await execute(
            conn,
            """INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,price_usd,monthly_sales,monthly_revenue,review_count,rating,bsr_rank) VALUES(:t,:id,:p,:month,:kind,:start,:end,:observed,:source,:row,:price_usd,:monthly_sales,:monthly_revenue,:review_count,:rating,:bsr_rank)""",
            **args,
        )
    # The most recent statistics period wins, rather than the last file uploaded.
    current = await one(
        conn,
        """SELECT id,revision FROM ps_product_monthly WHERE tenant_id=:t AND product_id=:p ORDER BY stat_month DESC,period_end DESC,observed_on DESC,updated_at DESC,id DESC LIMIT 1""",
        t=tenant,
        p=product_id,
    )
    await execute(
        conn,
        """UPDATE ps_products SET current_monthly_id=:mid,current_monthly_revision=:rev,current_result_id=NULL,current_group_rep_id=NULL,data_revision=data_revision+1,screen_generation=screen_generation+1,updated_at=now() WHERE tenant_id=:t AND id=:p AND (current_monthly_id IS DISTINCT FROM CAST(:mid AS uuid) OR current_monthly_revision IS DISTINCT FROM :rev)""",
        t=tenant,
        p=product_id,
        mid=str(current["id"]),
        rev=current["revision"],
    )
    return args["id"]
