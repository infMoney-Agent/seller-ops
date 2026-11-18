"""Durable DB jobs; Celery only transports IDs. All retries are idempotent."""

import asyncio
import logging
from app.workers.celery_app import celery_app
from . import db
from .api import (
    Context,
    resource,
    screen_job,
    save_scenario,
    storage,
    js,
    uid,
    BASE,
    FIELDS,
)
from .contracts import Selection, SaveProfit, ProfitInputs
from .monthly import upsert_monthly, base_for, product_with_metrics
from .domain import MAX_ROWS, parse_file, classify, digest, excel_text

log = logging.getLogger(__name__)


async def heartbeat(c, job, token, progress):
    result = await db.execute(
        c.conn,
        "UPDATE ps_jobs SET lease_until=now()+interval '5 minutes',updated_at=now(),progress=:p WHERE tenant_id=:t AND id=:id AND state='running' AND lease_token=:token",
        t=c.tenant,
        id=str(job["id"]),
        token=token,
        p=progress,
    )
    if result.rowcount != 1:
        raise RuntimeError("Task lease lost")


async def parse_job(c, job, token):
    payload = job["payload"]
    id = payload["import_id"]
    async with db.transaction(c.tenant) as conn:
        c.conn = conn
        p = await resource(c, "ps_imports", id)
        if p["parse_generation"] != payload["generation"]:
            return {"superseded": True}
        if p["state"] in ("importing", "completed", "partial"):
            return {"already_committed": True}
        await db.execute(
            conn,
            "UPDATE ps_imports SET state='parsing' WHERE tenant_id=:t AND id=:id",
            t=c.tenant,
            id=id,
        )
        await db.execute(
            conn,
            "DELETE FROM ps_import_rows WHERE tenant_id=:t AND import_id=:id AND product_id IS NULL",
            t=c.tenant,
            id=id,
        )
    data = (storage() / p["storage_key"]).read_bytes()
    iterator = iter(parse_file(data, p["file_name"], p["mapping"]))
    total = 0
    valid = 0
    invalid = 0
    while True:
        chunk = []
        for _ in range(500):
            row = next(iterator, None)
            if row is None:
                break
            chunk.append(row)
        if not chunk:
            break
        total += len(chunk)
        if total > MAX_ROWS:
            raise ValueError("每批最多10万行，请拆分文件")
        async with db.transaction(c.tenant) as conn:
            c.conn = conn
            current = await resource(c, "ps_imports", id)
            if current["parse_generation"] != payload["generation"]:
                return {"superseded": True}
            for row in chunk:
                state = "invalid" if row["errors"] else "valid"
                valid += state == "valid"
                invalid += state == "invalid"
                await db.execute(
                    conn,
                    "INSERT INTO ps_import_rows(tenant_id,id,import_id,member_path,sheet_name,row_no,state,raw_data,normalized_data,errors) VALUES(:t,:id,:i,:m,:s,:r,:state,CAST(:raw AS jsonb),CAST(:norm AS jsonb),CAST(:errors AS jsonb)) ON CONFLICT(tenant_id,import_id,member_path,sheet_name,row_no) DO NOTHING",
                    t=c.tenant,
                    id=uid(),
                    i=id,
                    m=row["member_path"],
                    s=row["sheet_name"],
                    r=row["row_no"],
                    state=state,
                    raw=js(row["raw_data"]),
                    norm=js(row["normalized_data"]),
                    errors=js(row["errors"]),
                )
            await heartbeat(c, job, token, min(95, total // 1000))
    async with db.transaction(c.tenant) as conn:
        counts = {"total": total, "valid": valid, "invalid": invalid}
        c.conn = conn
        await db.execute(
            conn,
            "UPDATE ps_imports SET state='preview',counts=CAST(:counts AS jsonb) WHERE tenant_id=:t AND id=:id AND parse_generation=:g",
            t=c.tenant,
            id=id,
            g=payload["generation"],
            counts=js(counts),
        )
    return counts


PRODUCT_FIELDS = [
    "title",
    "title_zh",
    "image_url",
    "category",
    "brand",
    "seller_id",
    "seller_name",
    "seller_region",
]


async def import_job(c, job, token):
    id = job["payload"]["import_id"]
    count = 0
    async with db.transaction(c.tenant) as conn:
        c.conn = conn
        p = await resource(c, "ps_imports", id)
        if p["parse_generation"] != job["payload"]["generation"]:
            return {"superseded": True}
        if not p["observed_on"]:
            raise ValueError("缺少观察日期")
        await db.execute(
            conn,
            "UPDATE ps_imports SET state='importing' WHERE tenant_id=:t AND id=:id",
            t=c.tenant,
            id=id,
        )
    while True:
        async with db.transaction(c.tenant) as conn:
            c.conn = conn
            chunk = await db.rows(
                conn,
                "SELECT * FROM ps_import_rows WHERE tenant_id=:t AND import_id=:id AND state='valid' ORDER BY member_path,sheet_name,row_no,id LIMIT 500",
                t=c.tenant,
                id=id,
            )
            if not chunk:
                break
            for row in chunk:
                n = row["normalized_data"]
                pid = uid()
                await db.execute(
                    conn,
                    "INSERT INTO ps_products(tenant_id,id,asin,title) VALUES(:t,:id,:asin,:title) ON CONFLICT(tenant_id,marketplace,asin) DO NOTHING",
                    t=c.tenant,
                    id=pid,
                    asin=n["asin"],
                    title=n["title"],
                )
                product = await db.one(
                    conn,
                    "SELECT * FROM ps_products WHERE tenant_id=:t AND marketplace=:market AND asin=:asin",
                    t=c.tenant,
                    market=p["marketplace"],
                    asin=n["asin"],
                )
                pid = str(product["id"])
                old = None
                if product["current_row_id"]:
                    old = await db.one(
                        conn,
                        "SELECT i.observed_on,i.created_at,r.member_path,r.sheet_name,r.row_no,r.id FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=:t AND r.id=:id",
                        t=c.tenant,
                        id=str(product["current_row_id"]),
                    )
                incoming = (
                    p["observed_on"],
                    p["created_at"],
                    row["member_path"],
                    row["sheet_name"],
                    row["row_no"],
                    str(row["id"]),
                )
                previous = (
                    (
                        old["observed_on"],
                        old["created_at"],
                        old["member_path"],
                        old["sheet_name"],
                        old["row_no"],
                        str(old["id"]),
                    )
                    if old
                    else None
                )
                await db.execute(
                    conn,
                    "UPDATE ps_import_rows SET state='committed',product_id=:p WHERE tenant_id=:t AND id=:id",
                    t=c.tenant,
                    id=str(row["id"]),
                    p=pid,
                )
                await upsert_monthly(conn, c.tenant, pid, p, row)
                if previous is None or incoming > previous:
                    params = {k: n.get(k) for k in PRODUCT_FIELDS}
                    from decimal import Decimal

                    for key in ():
                        if params[key] is not None:
                            params[key] = Decimal(str(params[key]))
                    await db.execute(
                        conn,
                        "UPDATE ps_products SET "
                        + ",".join(f"{key}=:{key}" for key in PRODUCT_FIELDS)
                        + ",specs=CAST(:specs AS jsonb),current_row_id=:row,current_result_id=NULL,current_group_rep_id=NULL,data_revision=data_revision+1,screen_generation=screen_generation+1,updated_at=now() WHERE tenant_id=:t AND id=:id",
                        **params,
                        specs=js(n.get("specs", {})),
                        row=str(row["id"]),
                        t=c.tenant,
                        id=pid,
                    )
                count += 1
            await heartbeat(
                c,
                job,
                token,
                min(95, int(count / max(1, p["counts"].get("valid", 1)) * 95)),
            )
    async with db.transaction(c.tenant) as conn:
        c.conn = conn
        await db.execute(
            conn,
            "UPDATE ps_imports SET state=:s,committed_at=now() WHERE tenant_id=:t AND id=:id",
            t=c.tenant,
            id=id,
            s="partial" if p["counts"].get("invalid") else "completed",
        )
        screen = await screen_job(
            c,
            Selection(filters={"import_id": id}),
            "import-screen:" + id + ":" + str(job["payload"]["generation"]),
        )
    return {
        "import_id": id,
        "committed": p["counts"].get("valid", count),
        "invalid": p["counts"].get("invalid", 0),
        "screen": screen,
    }


async def screen_task(c, job, token):
    payload = job["payload"]
    done = 0
    skipped = 0
    async with db.transaction() as conn:
        c.conn = conn
        policy = await resource(c, "ps_policies", payload["policy_id"])
        run = await db.one(
            conn,
            "SELECT * FROM ps_screen_runs WHERE tenant_id=:t AND job_id=:id",
            t=c.tenant,
            id=str(job["id"]),
        )
    for start in range(0, len(payload["targets"]), 100):
        async with db.transaction(c.tenant) as conn:
            c.conn = conn
            active = await db.one(
                conn,
                "SELECT id FROM ps_policies WHERE tenant_id=:t AND active",
                t=c.tenant,
            )
            for target in payload["targets"][start : start + 100]:
                p = await resource(c, "ps_products", target["id"])
                if (
                    not active
                    or str(active["id"]) != payload["policy_id"]
                    or p["data_revision"] != target["data_revision"]
                    or p["screen_generation"] != target["screen_generation"]
                ):
                    skipped += 1
                    continue
                occurrence = await db.one(
                    conn,
                    "SELECT count(DISTINCT i.source_period) AS n FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=:t AND r.product_id=:p",
                    t=c.tenant,
                    p=str(p["id"]),
                )
                result = classify(
                    {**p, "occurrences": occurrence["n"]}, policy["config"]
                )
                result["evidence"].append(
                    {
                        "rule": "商品与分期来源快照",
                        "profile_source_row_id": p["current_row_id"],
                        "monthly_id": p["monthly_id"],
                        "monthly_revision": p["monthly_revision"],
                        "monthly_source_row_id": p["monthly_source_row_id"],
                        "price_usd": p["price_usd"],
                        "monthly_sales": p["monthly_sales"],
                    }
                )
                rid = uid()
                grouping = policy["config"].get("grouping", {})
                seller = p["seller_id"] or "unknown:" + p["asin"]
                group = digest(
                    [
                        p["marketplace"],
                        result["direction"],
                        seller if grouping.get("require_same_seller", True) else "",
                        p["asin"]
                        if not grouping.get("enabled", True) or not p["seller_id"]
                        else "",
                    ]
                )
                await db.execute(
                    conn,
                    "INSERT INTO ps_screen_results(tenant_id,id,run_id,product_id,source_row_id,data_revision,generation,decision,recommendation,direction,group_key,representative_id,reasons,evidence,monthly_id,monthly_revision) VALUES(:t,:id,:run,:p,:row,:v,:g,:d,:rec,:direction,:group,:p,CAST(:r AS jsonb),CAST(:e AS jsonb),:mid,:mrev) ON CONFLICT(tenant_id,run_id,product_id) DO NOTHING",
                    t=c.tenant,
                    id=rid,
                    run=str(run["id"]),
                    p=str(p["id"]),
                    row=str(p["monthly_source_row_id"] or p["current_row_id"]),
                    mid=str(p["monthly_id"]) if p["monthly_id"] else None,
                    mrev=p["monthly_revision"],
                    v=p["data_revision"],
                    g=p["screen_generation"],
                    d=result["decision"],
                    rec=result["recommendation"],
                    direction=result["direction"],
                    group=group,
                    r=js(result["reasons"]),
                    e=js(result["evidence"]),
                )
                resultrow = await db.one(
                    conn,
                    "SELECT id FROM ps_screen_results WHERE tenant_id=:t AND run_id=:run AND product_id=:p",
                    t=c.tenant,
                    run=str(run["id"]),
                    p=str(p["id"]),
                )
                await db.execute(
                    conn,
                    "UPDATE ps_products SET current_result_id=:r,current_group_rep_id=NULL WHERE tenant_id=:t AND id=:id",
                    t=c.tenant,
                    id=str(p["id"]),
                    r=str(resultrow["id"]),
                )
                done += 1
            await heartbeat(
                c,
                job,
                token,
                int(
                    (start + len(payload["targets"][start : start + 100]))
                    / max(1, len(payload["targets"]))
                    * 95
                ),
            )
    async with db.transaction(c.tenant) as conn:
        c.conn = conn
        # Include all current results, including untouched products from previous imports.
        await db.execute(
            conn,
            "WITH reps AS (SELECT s.group_key,(array_agg(p.id ORDER BY (p.review_status='kept') DESC,m.monthly_sales DESC NULLS LAST,p.id))[1] AS rep FROM ps_products p LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id JOIN ps_screen_results s ON s.tenant_id=p.tenant_id AND s.id=p.current_result_id WHERE p.tenant_id=:t AND s.decision='passed' AND s.recommendation='regular' GROUP BY s.group_key) UPDATE ps_products p SET current_group_rep_id=reps.rep FROM reps,ps_screen_results s WHERE p.tenant_id=:t AND s.tenant_id=p.tenant_id AND s.id=p.current_result_id AND s.group_key=reps.group_key AND s.decision='passed' AND s.recommendation='regular'",
            t=c.tenant,
        )
        # Finalize only this run's snapshot. Earlier run history is never rewritten.
        await db.execute(
            conn,
            "UPDATE ps_screen_results s SET representative_id=p.current_group_rep_id FROM ps_products p WHERE s.tenant_id=:t AND s.run_id=:run AND p.tenant_id=s.tenant_id AND p.current_result_id=s.id AND p.current_group_rep_id IS NOT NULL",
            t=c.tenant,
            run=str(run["id"]),
        )
        result = {"screened": done, "superseded": skipped}
        await db.execute(
            conn,
            "UPDATE ps_screen_runs SET summary=CAST(:s AS jsonb) WHERE tenant_id=:t AND id=:id",
            t=c.tenant,
            id=str(run["id"]),
            s=js(result),
        )
    return result


async def profits_task(c, job, token):
    items = []
    for index, target in enumerate(job["payload"]["targets"]):
        async with db.transaction(c.tenant) as conn:
            c.conn = conn
            old = await db.one(
                conn,
                "SELECT result FROM ps_jobs WHERE tenant_id=:t AND id=:id",
                t=c.tenant,
                id=str(job["id"]),
            )
            saved = (old["result"] or {}).get("items", [])
            if any(x["id"] == target["id"] for x in saved):
                items = saved
                continue
            p = await product_with_metrics(
                conn, c.tenant, target["id"], target.get("monthly_id")
            )
            if p["data_revision"] != target["data_revision"] or p[
                "monthly_revision"
            ] != target.get("monthly_revision"):
                item = {"id": target["id"], "success": False, "reason": "商品已更新"}
            else:
                inputs = {**job["payload"]["inputs"]}
                if (
                    inputs.get("sale_price_usd") in (None, "")
                    and p["price_usd"] is not None
                ):
                    inputs["sale_price_usd"] = str(p["price_usd"])
                result = await save_scenario(
                    c,
                    p,
                    SaveProfit(
                        expected_data_revision=p["data_revision"],
                        monthly_id=target.get("monthly_id"),
                        expected_monthly_revision=target.get("monthly_revision"),
                        inputs=ProfitInputs(**inputs),
                        template_id=job["payload"].get("template_id"),
                    ),
                )
                item = {
                    "id": target["id"],
                    "success": True,
                    "scenario_id": result["id"],
                    "state": result["output"]["state"],
                }
            items = saved + [item]
            await db.execute(
                conn,
                "UPDATE ps_jobs SET result=CAST(:r AS jsonb) WHERE tenant_id=:t AND id=:id",
                t=c.tenant,
                id=str(job["id"]),
                r=js({"items": items}),
            )
            await heartbeat(
                c,
                job,
                token,
                int((index + 1) / max(1, len(job["payload"]["targets"])) * 95),
            )
    return {"items": items}


async def export_task(c, job, token):
    from openpyxl import Workbook

    ids = job["payload"]["product_ids"]
    book = Workbook(write_only=True)
    sheet = book.create_sheet("产品初筛")
    fields = [
        "asin",
        "title",
        "title_zh",
        "category",
        "seller_name",
        "seller_region",
        "price_usd",
        "monthly_sales",
        "decision",
        "recommendation",
        "direction",
        "reasons",
        "review_status",
        "review_reason",
        "review_note",
        "profit_output",
        "data_revision",
    ]
    sheet.append(
        [
            "ASIN",
            "商品标题",
            "中文标题",
            "类目",
            "卖家",
            "地区",
            "价格美元",
            "月销量",
            "机器结果",
            "推荐类型",
            "方向",
            "机器原因",
            "人工结果",
            "人工原因",
            "备注",
            "利润方案结果",
            "数据版本",
        ]
    )
    monthly_sheet = book.create_sheet("月度数据")
    monthly_sheet.append(
        [
            "ASIN",
            "月份",
            "统计口径",
            "统计开始",
            "统计结束",
            "观察日期",
            "来源",
            "价格USD",
            "月销量",
            "月销售额USD",
            "评分数",
            "评分",
            "BSR",
            "版本",
        ]
    )
    source = book.create_sheet("来源记录")
    source.append(["ASIN", "文件", "来源期", "观察日期", "ZIP成员", "工作表", "行号"])
    async with db.transaction() as conn:
        c.conn = conn
        await db.execute(
            conn, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"
        )
        for offset in range(0, len(ids), 500):
            chunk = ids[offset : offset + 500]
            products = await db.rows(
                conn,
                "SELECT "
                + FIELDS
                + base_for(BASE, job["payload"].get("filters", {}))
                + " WHERE p.tenant_id=:t AND p.id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb))) ORDER BY p.id",
                t=c.tenant,
                ids=js(chunk),
            )
            for p in products:
                sheet.append(
                    [
                        excel_text(js(p.get(k)))
                        if isinstance(p.get(k), (dict, list))
                        else excel_text(p.get(k))
                        for k in fields
                    ]
                )
            monthly_rows = await db.rows(
                conn,
                "SELECT p.asin,m.stat_month,m.period_type,m.period_start,m.period_end,m.observed_on,m.source_type,m.price_usd,m.monthly_sales,m.monthly_revenue,m.review_count,m.rating,m.bsr_rank,m.revision FROM ps_product_monthly m JOIN ps_products p ON p.tenant_id=m.tenant_id AND p.id=m.product_id WHERE m.tenant_id=:t AND m.product_id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb))) ORDER BY p.asin,m.stat_month,m.period_end",
                t=c.tenant,
                ids=js(chunk),
            )
            for metric in monthly_rows:
                monthly_sheet.append([excel_text(v) for v in metric.values()])
            observations = await db.rows(
                conn,
                "SELECT p.asin,i.file_name,i.source_period,i.observed_on,r.member_path,r.sheet_name,r.row_no FROM ps_import_rows r JOIN ps_products p ON p.tenant_id=r.tenant_id AND p.id=r.product_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=:t AND r.product_id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb))) ORDER BY p.asin,r.id",
                t=c.tenant,
                ids=js(chunk),
            )
            for r in observations:
                source.append([excel_text(v) for v in r.values()])
            async with db.transaction(c.tenant) as heartbeat_conn:
                hc = Context(c.tenant, c.user, c.role, heartbeat_conn)
                await heartbeat(
                    hc, job, token, int((offset + len(chunk)) / max(1, len(ids)) * 95)
                )
    key = f"{c.tenant}/exports/{job['id']}.xlsx"
    path = storage() / key
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(".tmp")
    book.save(temp)
    temp.replace(path)
    return {"storage_key": key, "products": len(ids)}


async def run_job(tenant, id):
    token = uid()
    async with db.transaction(tenant) as conn:
        job = await db.one(
            conn,
            "UPDATE ps_jobs SET state='running',attempts=attempts+1,lease_token=:token,lease_until=now()+interval '5 minutes',updated_at=now() WHERE tenant_id=:t AND id=:id AND (state='queued' OR (state='running' AND lease_until<now())) RETURNING *",
            t=tenant,
            id=id,
            token=token,
        )
        if not job:
            return
    c = Context(tenant, job["created_by"], "member")
    try:
        result = await {
            "parse": parse_job,
            "import": import_job,
            "screen": screen_task,
            "profit_batch": profits_task,
            "export": export_task,
        }[job["kind"]](c, job, token)
        state = (
            "partial"
            if (job["kind"] == "import" and result.get("invalid"))
            or any(not i.get("success", True) for i in result.get("items", []))
            else "succeeded"
        )
        async with db.transaction(tenant) as conn:
            await db.execute(
                conn,
                "UPDATE ps_jobs SET state=:s,result=CAST(:r AS jsonb),progress=100,lease_until=NULL,updated_at=now() WHERE tenant_id=:t AND id=:id AND lease_token=:token",
                t=tenant,
                id=id,
                token=token,
                s=state,
                r=js(result),
            )
    except Exception as error:
        log.exception("Screening job failed tenant=%s job=%s", tenant, id)
        async with db.transaction(tenant) as conn:
            await db.execute(
                conn,
                "UPDATE ps_jobs SET state='failed',error=CAST(:e AS jsonb),lease_until=NULL,updated_at=now() WHERE tenant_id=:t AND id=:id AND lease_token=:token",
                t=tenant,
                id=id,
                token=token,
                e=js({"message": str(error)[:1000]}),
            )
            if job["kind"] in ("parse", "import"):
                await db.execute(
                    conn,
                    "UPDATE ps_imports SET state='failed' WHERE tenant_id=:t AND id=:id AND parse_generation=:g",
                    t=tenant,
                    id=job["payload"]["import_id"],
                    g=job["payload"]["generation"],
                )


async def dispatch():
    if not __import__("os").environ.get("SCREENING_DATABASE_URL"):
        return
    async with db.transaction() as conn:
        jobs = await db.rows(
            conn,
            "SELECT tenant_id,id FROM ps_jobs WHERE state='queued' OR (state='running' AND lease_until<now()) ORDER BY created_at LIMIT 100",
        )
    for job in jobs:
        execute_job.delay(job["tenant_id"], str(job["id"]))


def isolated(coroutine):
    # Celery creates an event loop per invocation: never reuse asyncpg pools across loops.
    async def main():
        try:
            return await coroutine
        finally:
            if db._engine is not None:
                await db._engine.dispose()
                db._engine = None

    return asyncio.run(main())


@celery_app.task(name="screening.execute", acks_late=True)
def execute_job(tenant, id):
    return isolated(run_job(tenant, id))


@celery_app.task(name="screening.dispatch")
def dispatch_jobs():
    return isolated(dispatch())
