import hashlib
import hmac
import json
import os
import time
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from uuid import UUID, uuid4
from fastapi import APIRouter, File, Form, HTTPException, Query, Request, UploadFile
from fastapi.encoders import jsonable_encoder
from fastapi.exceptions import RequestValidationError
from fastapi.responses import FileResponse, JSONResponse, Response
from fastapi.routing import APIRoute
from sqlalchemy.exc import IntegrityError
from . import contracts as C
from .db import transaction, rows, one, execute
from .monthly import CURRENT_JOIN, METRIC_SELECT, base_for, product_with_metrics
from .domain import MAX_BYTES, calculate, classify, digest, FORMULA_VERSION


def uid():
    return str(uuid4())


def js(value):
    return json.dumps(jsonable_encoder(value), ensure_ascii=False)


def fail(status, message, code=None):
    raise HTTPException(
        status, detail={"code": code or str(status), "message": message}
    )


def storage():
    path = Path(os.environ.get("SCREENING_STORAGE_DIR", "/data/screening")).resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path


@dataclass
class Context:
    tenant: int
    user: int
    role: str
    conn: object = None


def authenticate(request):
    secret = os.environ.get("SCREENING_INTERNAL_SECRET", "")
    if len(secret) < 32:
        fail(503, "初筛服务未配置", "not_configured")
    if not os.environ.get("SCREENING_DATABASE_URL"):
        fail(503, "初筛数据库未配置", "not_configured")
    try:
        tenant = int(request.headers["x-seller-workspace"])
        user = int(request.headers["x-seller-user"])
        stamp = int(request.headers["x-screening-time"])
        role = request.headers["x-seller-role"]
        if (
            min(tenant, user) <= 0
            or role not in ("owner", "member")
            or abs(time.time() - stamp) > 60
        ):
            raise ValueError()
    except (KeyError, ValueError):
        fail(401, "无效服务身份")
    target = request.url.path + ("?" + request.url.query if request.url.query else "")
    message = "\n".join(
        [str(stamp), request.method, target, str(user), str(tenant), role]
    )
    expected = hmac.new(secret.encode(), message.encode(), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(
        expected, request.headers.get("x-screening-signature", "")
    ):
        fail(401, "无效服务签名")
    return Context(tenant, user, role)


class ScreeningRoute(APIRoute):
    def get_route_handler(self):
        original = super().get_route_handler()

        async def handler(request):
            request_id = uid()
            try:
                ctx = authenticate(request)
                write = request.method not in ("GET", "HEAD")
                upload = request.method == "POST" and request.url.path.endswith(
                    "/imports"
                )
                key = request.headers.get("idempotency-key", "")
                if write and (not key or len(key) > 200):
                    fail(422, "请提供有效 Idempotency-Key")
                body = b""
                if write and not upload:
                    body = await request.body()
                    if len(body) > 256 * 1024:
                        fail(413, "请求超过256KB")
                fingerprint = digest(
                    [
                        ctx.user,
                        request.method,
                        request.url.path,
                        request.url.query,
                        body.decode("utf-8"),
                    ]
                )
                async with transaction(ctx.tenant if write else None) as conn:
                    ctx.conn = conn
                    request.state.screening = ctx
                    if write and not upload:
                        previous = await one(
                            conn,
                            "SELECT * FROM ps_requests WHERE tenant_id=:t AND request_key=:k",
                            t=ctx.tenant,
                            k=key,
                        )
                        if previous:
                            if previous["request_hash"] != fingerprint:
                                fail(409, "幂等键已用于不同请求")
                            return JSONResponse(
                                previous["result"]["body"],
                                status_code=previous["result"]["status"],
                            )
                    response = await original(request)
                    if write and not upload and response.status_code < 400:
                        await execute(
                            conn,
                            "INSERT INTO ps_requests(tenant_id,request_key,request_hash,result) VALUES(:t,:k,:h,CAST(:r AS jsonb))",
                            t=ctx.tenant,
                            k=key,
                            h=fingerprint,
                            r=js(
                                {
                                    "body": json.loads(response.body),
                                    "status": response.status_code,
                                }
                            ),
                        )
                return response
            except HTTPException as error:
                detail = (
                    error.detail
                    if isinstance(error.detail, dict)
                    else {"message": str(error.detail)}
                )
                return JSONResponse(
                    {
                        "code": str(error.status_code),
                        **detail,
                        "details": detail.get("details"),
                        "request_id": request_id,
                    },
                    status_code=error.status_code,
                )
            except RequestValidationError as error:
                return JSONResponse(
                    {
                        "code": "validation",
                        "message": "输入不符合要求",
                        "details": jsonable_encoder(
                            error.errors(), custom_encoder={ValueError: str}
                        ),
                        "request_id": request_id,
                    },
                    status_code=422,
                )
            except (ValueError, TimeoutError) as error:
                return JSONResponse(
                    {
                        "code": "validation",
                        "message": str(error),
                        "request_id": request_id,
                    },
                    status_code=422,
                )
            except IntegrityError:
                return JSONResponse(
                    {
                        "code": "conflict",
                        "message": "数据已变化或已存在，请刷新后重试",
                        "request_id": request_id,
                    },
                    status_code=409,
                )

        return handler


router = APIRouter(
    prefix="/screening", tags=["Product Screening"], route_class=ScreeningRoute
)


def ctx(request):
    return request.state.screening


def owner(context):
    if context.role != "owner":
        fail(403, "只有工作区所有者可以修改公共配置")


async def resource(c, table, id):
    if table not in {
        "ps_products",
        "ps_imports",
        "ps_policies",
        "ps_screen_runs",
        "ps_review_batches",
        "ps_jobs",
        "ps_profit_templates",
    }:
        raise ValueError("Unknown resource")
    if table == "ps_products":
        row = await product_with_metrics(c.conn, c.tenant, id)
        if not row:
            fail(404, "未找到记录")
        return row
    row = await one(
        c.conn,
        f"SELECT * FROM {table} WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(id),
    )
    if not row:
        fail(404, "未找到记录")
    return row


async def ensure_policy(c):
    current = await one(
        c.conn, "SELECT * FROM ps_policies WHERE tenant_id=:t AND active", t=c.tenant
    )
    if current:
        return current
    config = json.loads((Path(__file__).parent / "default-policy.json").read_text())
    await execute(
        c.conn,
        "INSERT INTO ps_policies(tenant_id,id,version,name,config,config_hash,active,created_by) VALUES(:t,:id,1,:n,CAST(:c AS jsonb),:h,true,:u)",
        t=c.tenant,
        id=uid(),
        n=config.get("strategy_name", "资料库初始规则"),
        c=js(config),
        h=digest(config),
        u=c.user,
    )
    return await one(
        c.conn, "SELECT * FROM ps_policies WHERE tenant_id=:t AND active", t=c.tenant
    )


async def new_job(c, kind, payload, key):
    old = await one(
        c.conn,
        "SELECT * FROM ps_jobs WHERE tenant_id=:t AND kind=:k AND idempotency_key=:key",
        t=c.tenant,
        k=kind,
        key=key,
    )
    if old:
        if digest(old["payload"]) != digest(payload):
            fail(409, "任务幂等键冲突")
        return {"job_id": str(old["id"]), "state": old["state"]}
    id = uid()
    await execute(
        c.conn,
        "INSERT INTO ps_jobs(tenant_id,id,kind,payload,idempotency_key,created_by) VALUES(:t,:id,:k,CAST(:p AS jsonb),:key,:u)",
        t=c.tenant,
        id=id,
        k=kind,
        p=js(payload),
        key=key,
        u=c.user,
    )
    return {"job_id": id, "state": "queued"}


BASE = (
    """ FROM ps_products p """
    + CURRENT_JOIN
    + """ LEFT JOIN ps_screen_results s ON s.tenant_id=p.tenant_id AND s.id=p.current_result_id
 LEFT JOIN (SELECT ref.tenant_id,rs.direction,count(*) AS n FROM ps_products ref JOIN ps_screen_results rs ON rs.tenant_id=ref.tenant_id AND rs.id=ref.current_result_id WHERE ref.review_status<>'pending' GROUP BY ref.tenant_id,rs.direction) refs ON refs.tenant_id=p.tenant_id AND refs.direction=s.direction
 LEFT JOIN LATERAL (SELECT output,product_data_revision,monthly_id,monthly_revision FROM ps_profit_scenarios q WHERE q.tenant_id=p.tenant_id AND q.product_id=p.id AND q.monthly_id IS NOT DISTINCT FROM m.id ORDER BY created_at DESC,id DESC LIMIT 1) profit ON true """
)
FIELDS = (
    """p.*, """
    + METRIC_SELECT
    + """,s.decision,s.recommendation,s.direction,s.group_key,COALESCE(p.current_group_rep_id,s.representative_id) AS representative_id,s.reasons,s.evidence,
 profit.output AS profit_output,profit.product_data_revision AS profit_data_revision,profit.monthly_revision AS profit_monthly_revision,
 COALESCE(refs.n>CASE WHEN p.review_status<>'pending' THEN 1 ELSE 0 END,false) AS has_reference"""
)


def filter_base(params):
    # Counts and page IDs do not need profit/reference enrichment unless filtering by it.
    prefix, rest = BASE.split(" LEFT JOIN (SELECT ref.", 1)
    reference, profit = rest.split(" LEFT JOIN LATERAL (SELECT output", 1)
    base = prefix
    if params.get("has_reference") in ("true", "false"):
        base += " LEFT JOIN (SELECT ref." + reference
    if params.get("profit_min") not in (None, "") or params.get("profit_max") not in (
        None,
        "",
    ):
        base += " LEFT JOIN LATERAL (SELECT output" + profit
    return base_for(base, params)


def filters(params, t):
    clauses = ["p.tenant_id=:t"]
    args = {"t": t}
    for name, column in [
        ("review_status", "p.review_status"),
        ("machine", "COALESCE(s.decision,'unscreened')"),
        ("recommendation", "s.recommendation"),
        ("category", "p.category"),
        ("seller_region", "p.seller_region"),
        ("direction", "s.direction"),
    ]:
        value = params.get(name)
        if value:
            clauses.append(f"{column}=:{name}")
            args[name] = value
    if (
        params.get("stat_month")
        or params.get("source_type")
        or params.get("period_type")
    ):
        clauses.append("m.id IS NOT NULL")
    if params.get("q"):
        clauses.append("(p.asin ILIKE :q OR p.title ILIKE :q OR p.title_zh ILIKE :q)")
        args["q"] = "%" + str(params["q"])[:200] + "%"
    if params.get("import_id"):
        clauses.append(
            "EXISTS(SELECT 1 FROM ps_import_rows ir WHERE ir.tenant_id=p.tenant_id AND ir.product_id=p.id AND ir.import_id=:import_id)"
        )
        args["import_id"] = str(UUID(str(params["import_id"])))
    if params.get("has_reference") in ("true", "false"):
        exists = "COALESCE(refs.n>CASE WHEN p.review_status<>'pending' THEN 1 ELSE 0 END,false)"
        clauses.append(exists if params["has_reference"] == "true" else "NOT " + exists)
    for name, op in [("profit_min", ">="), ("profit_max", "<=")]:
        if params.get(name) not in (None, ""):
            from decimal import Decimal

            value = Decimal(str(params[name]))
            if not value.is_finite():
                raise ValueError("利润范围无效")
            clauses.append(f"(profit.output->>'profit_usd')::numeric {op} :{name}")
            args[name] = value
    return " AND ".join(clauses), args


async def selected(c, selection):
    where, args = filters(selection.filters, c.tenant)
    if selection.product_ids is not None:
        ids = [str(v) for v in selection.product_ids]
        where += " AND p.id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb)))"
        args["ids"] = js(ids)
    return await rows(
        c.conn,
        "SELECT p.id,p.data_revision,m.id AS monthly_id,m.revision AS monthly_revision"
        + filter_base(selection.filters)
        + " WHERE "
        + where
        + " ORDER BY p.id",
        **args,
    )


MONTH_FILTER_DOCS = [
    {
        "name": "stat_month",
        "in": "query",
        "required": False,
        "description": "经营数据月份 YYYY-MM；不传则各商品最新一期",
        "schema": {"type": "string"},
    },
    {
        "name": "period_type",
        "in": "query",
        "required": False,
        "description": "自然月、滚动30天或自定义；指定月份时默认自然月",
        "schema": {
            "type": "string",
            "enum": ["calendar_month", "rolling_30d", "custom"],
        },
    },
    {
        "name": "source_type",
        "in": "query",
        "required": False,
        "description": "sellersprite、manual、other、legacy_import或明确来源代码",
        "schema": {"type": "string"},
    },
]


@router.get("/products", openapi_extra={"parameters": MONTH_FILTER_DOCS})
async def products(
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    c = ctx(request)
    params = request.query_params
    where, args = filters(params, c.tenant)
    collapsed = params.get("show_merged", "false") != "true"
    # Choose a representative from the filtered result, so filters never hide the entire group.
    rank = "row_number() OVER(PARTITION BY CASE WHEN recommendation='exploration' OR group_key IS NULL THEN id::text ELSE group_key END ORDER BY CASE WHEN id=representative_id THEN 0 ELSE 1 END,id) AS group_rank"
    lean = "p.id,m.monthly_sales,m.price_usd,p.updated_at,s.recommendation,s.group_key,COALESCE(p.current_group_rep_id,s.representative_id) AS representative_id"
    cte = (
        "WITH filtered AS (SELECT "
        + lean
        + filter_base(request.query_params)
        + " WHERE "
        + where
        + "), ranked AS (SELECT *, "
        + rank
        + " FROM filtered) "
    )
    condition = " WHERE group_rank=1" if collapsed else ""
    count = await one(
        c.conn, cte + "SELECT count(*) AS n FROM ranked" + condition, **args
    )
    order = {
        "sales": "monthly_sales DESC NULLS LAST",
        "price": "price_usd DESC NULLS LAST",
        "updated": "updated_at DESC",
    }.get(params.get("sort", "updated"), "updated_at DESC")
    paging = (
        cte
        + "SELECT id FROM ranked"
        + condition
        + f" ORDER BY {order},id DESC LIMIT :limit OFFSET :offset"
    )
    # Enrich only this page; large JSON fields and profit/reference joins must not be sorted for every product.
    page_ids = await rows(
        c.conn, paging, **args, limit=page_size, offset=(page - 1) * page_size
    )
    ids = [str(item["id"]) for item in page_ids]
    enriched = (
        await rows(
            c.conn,
            "SELECT "
            + FIELDS
            + base_for(BASE, request.query_params, ids)
            + " WHERE p.tenant_id=:t AND p.id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb)))",
            t=c.tenant,
            ids=js(ids),
        )
        if ids
        else []
    )
    by_id = {str(item["id"]): item for item in enriched}
    items = [by_id[id] for id in ids]
    return {"items": items, "total": count["n"], "page": page, "page_size": page_size}


@router.get("/summary", openapi_extra={"parameters": MONTH_FILTER_DOCS})
async def summary(request: Request):
    c = ctx(request)
    where, args = filters(request.query_params, c.tenant)
    value = await one(
        c.conn,
        "SELECT count(*) AS products,count(*) FILTER(WHERE review_status='pending') AS pending,count(*) FILTER(WHERE review_status='kept') AS kept,count(*) FILTER(WHERE review_status='excluded') AS excluded,count(*) FILTER(WHERE review_status='unsure') AS unsure,count(DISTINCT CASE WHEN recommendation='exploration' OR group_key IS NULL THEN p.id::text ELSE group_key END) AS displayed"
        + filter_base(request.query_params)
        + " WHERE "
        + where,
        **args,
    )
    raw = await one(
        c.conn,
        "SELECT count(*) AS n FROM ps_import_rows WHERE tenant_id=:t AND product_id IN (SELECT p.id"
        + filter_base(request.query_params)
        + " WHERE "
        + where
        + ")",
        **args,
    )
    return {**value, "source_rows": raw["n"]}


@router.get("/products/{id}", openapi_extra={"parameters": MONTH_FILTER_DOCS})
async def product(id: UUID, request: Request):
    c = ctx(request)
    await resource(c, "ps_products", id)
    p = await one(
        c.conn,
        "SELECT "
        + FIELDS
        + base_for(BASE, request.query_params, [str(id)])
        + " WHERE p.tenant_id=:t AND p.id=:id",
        t=c.tenant,
        id=str(id),
    )
    return p


@router.get("/months")
async def month_options(request: Request):
    c = ctx(request)
    return {
        "items": await rows(
            c.conn,
            "SELECT stat_month,period_type,source_type,count(*) AS products FROM ps_product_monthly WHERE tenant_id=:t GROUP BY stat_month,period_type,source_type ORDER BY stat_month DESC,period_type,source_type",
            t=c.tenant,
        )
    }


@router.get("/products/{id}/monthly")
async def monthly_history(
    id: UUID,
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    c = ctx(request)
    await resource(c, "ps_products", id)
    n = await one(
        c.conn,
        "SELECT count(*) AS n FROM ps_product_monthly WHERE tenant_id=:t AND product_id=:p",
        t=c.tenant,
        p=str(id),
    )
    items = await rows(
        c.conn,
        "SELECT m.*,i.file_name,i.source_period,r.member_path,r.sheet_name,r.row_no FROM ps_product_monthly m JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE m.tenant_id=:t AND m.product_id=:p ORDER BY m.stat_month DESC,m.period_end DESC,m.observed_on DESC,m.id DESC LIMIT :l OFFSET :o",
        t=c.tenant,
        p=str(id),
        l=page_size,
        o=(page - 1) * page_size,
    )
    from decimal import Decimal

    return {
        "items": jsonable_encoder(items, custom_encoder={Decimal: str}),
        "total": n["n"],
        "page": page,
        "page_size": page_size,
    }


@router.get("/products/{id}/observations")
async def observations(
    id: UUID,
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    c = ctx(request)
    await resource(c, "ps_products", id)
    return await paginated(
        c, "ps_import_rows", "product_id=:pid", {"pid": str(id)}, page, page_size
    )


@router.get("/products/{id}/history")
async def history(
    id: UUID,
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    c = ctx(request)
    await resource(c, "ps_products", id)
    return await paginated(
        c, "ps_review_events", "product_id=:pid", {"pid": str(id)}, page, page_size
    )


async def paginated(c, table, where="true", args=None, page=1, page_size=50):
    args = {"t": c.tenant, **(args or {})}
    n = await one(
        c.conn,
        f"SELECT count(*) AS n FROM {table} WHERE tenant_id=:t AND " + where,
        **args,
    )
    order = "row_no,id" if table == "ps_import_rows" else "created_at DESC,id DESC"
    items = await rows(
        c.conn,
        f"SELECT * FROM {table} WHERE tenant_id=:t AND "
        + where
        + f" ORDER BY {order} LIMIT :limit OFFSET :offset",
        **args,
        limit=page_size,
        offset=(page - 1) * page_size,
    )
    return {"items": items, "total": n["n"], "page": page, "page_size": page_size}


@router.get("/products/{id}/references")
async def references(id: UUID, request: Request):
    c = ctx(request)
    await resource(c, "ps_products", id)
    items = await rows(
        c.conn,
        "SELECT "
        + FIELDS
        + BASE
        + " WHERE p.tenant_id=:t AND p.id<>:id AND p.review_status<>'pending' AND s.direction=(SELECT s.direction FROM ps_products p JOIN ps_screen_results s ON s.tenant_id=p.tenant_id AND s.id=p.current_result_id WHERE p.tenant_id=:t AND p.id=:id) ORDER BY p.reviewed_at DESC,p.id LIMIT 100",
        t=c.tenant,
        id=str(id),
    )
    return {"items": items}


@router.get(
    "/groups/{group_key}/products", openapi_extra={"parameters": MONTH_FILTER_DOCS}
)
async def group(
    group_key: str,
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    c = ctx(request)
    where, args = filters(request.query_params, c.tenant)
    args["g"] = group_key
    where += " AND s.group_key=:g"
    base = base_for(BASE, request.query_params)
    items = await rows(
        c.conn,
        "SELECT "
        + FIELDS
        + base
        + " WHERE "
        + where
        + " ORDER BY p.id LIMIT :l OFFSET :o",
        **args,
        l=page_size,
        o=(page - 1) * page_size,
    )
    n = await one(c.conn, "SELECT count(*) AS n" + base + " WHERE " + where, **args)
    return {"items": items, "total": n["n"], "page": page, "page_size": page_size}


async def review_write(c, p, status, reason, note, batch=None, undo_of=None):
    before = {
        k: p[k]
        for k in (
            "review_status",
            "review_reason",
            "review_note",
            "reviewed_by",
            "reviewed_at",
        )
    }
    after = {
        "review_status": status,
        "review_reason": reason,
        "review_note": note,
        "reviewed_by": c.user,
        "reviewed_at": datetime.now(timezone.utc),
    }
    await execute(
        c.conn,
        "UPDATE ps_products SET review_status=:s,review_reason=:r,review_note=:n,reviewed_by=:u,reviewed_at=now(),review_revision=review_revision+1,updated_at=now() WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(p["id"]),
        s=status,
        r=reason,
        n=note,
        u=c.user,
    )
    await execute(
        c.conn,
        "INSERT INTO ps_review_events(tenant_id,id,product_id,batch_id,undo_of,actor_id,before_revision,after_revision,before_value,after_value) VALUES(:t,:id,:p,:b,:undo,:u,:v,:next,CAST(:before AS jsonb),CAST(:after AS jsonb))",
        t=c.tenant,
        id=uid(),
        p=str(p["id"]),
        b=batch,
        undo=undo_of,
        u=c.user,
        v=p["review_revision"],
        next=p["review_revision"] + 1,
        before=js(before),
        after=js(after),
    )


@router.patch("/products/{id}/review")
async def review(id: UUID, body: C.Review, request: Request):
    c = ctx(request)
    p = await resource(c, "ps_products", id)
    if (
        p["review_revision"] != body.expected_review_revision
        or p["data_revision"] != body.expected_data_revision
    ):
        fail(409, "商品已变化，请刷新后复核")
    await review_write(c, p, body.status, body.reason, body.note)
    return {"id": str(id), "review_revision": p["review_revision"] + 1}


@router.post("/imports", status_code=202)
async def upload(
    request: Request,
    file: UploadFile = File(...),
    source_period: str = Form(""),
    observed_on: str = Form(""),
    source_type: str = Form("sellersprite"),
    period_type: str = Form("calendar_month"),
    period_start: str = Form(""),
    period_end: str = Form(""),
):
    c = ctx(request)
    name = Path(file.filename or "").name
    if Path(name).suffix.lower() not in (".xlsx", ".csv", ".zip"):
        fail(422, "仅支持 XLSX、CSV、ZIP")
    if len(source_period) > 80:
        fail(422, "来源期过长")
    from datetime import date

    observed = date.fromisoformat(observed_on) if observed_on else None
    metadata = C.Mapping(
        source_period=source_period,
        observed_on=observed,
        source_type=source_type,
        period_type=period_type,
        period_start=period_start or None,
        period_end=period_end or None,
    )
    bounds = {
        "source": metadata.source_type,
        "kind": metadata.period_type,
        "start": metadata.period_start,
        "end": metadata.period_end,
    }
    hash = hashlib.sha256()
    count = 0
    id = uid()
    key = f"{c.tenant}/sources/{id}{Path(name).suffix.lower()}"
    path = storage() / key
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        with path.open("xb") as output:
            while chunk := await file.read(1024 * 1024):
                count += len(chunk)
                if count > MAX_BYTES:
                    fail(413, "文件超过30MB")
                hash.update(chunk)
                output.write(chunk)
        if not count:
            fail(422, "文件为空")
        sha = hash.hexdigest()
        mh = digest({})
        request_key = request.headers["idempotency-key"]
        upload_hash = digest(
            [
                c.user,
                "upload",
                sha,
                name,
                source_period,
                str(observed),
                jsonable_encoder(bounds),
            ]
        )
        previous = await one(
            c.conn,
            "SELECT * FROM ps_requests WHERE tenant_id=:t AND request_key=:k",
            t=c.tenant,
            k=request_key,
        )
        if previous:
            if previous["request_hash"] != upload_hash:
                fail(409, "幂等键已用于不同上传")
            path.unlink()
            return previous["result"]["body"]
        old = await one(
            c.conn,
            "SELECT * FROM ps_imports WHERE tenant_id=:t AND file_sha256=:h AND source_period=:p AND mapping_hash=:m AND observed_on IS NOT DISTINCT FROM CAST(:d AS date) AND source_type=:source AND period_type=:kind AND period_start IS NOT DISTINCT FROM CAST(:start AS date) AND period_end IS NOT DISTINCT FROM CAST(:end AS date)",
            t=c.tenant,
            h=sha,
            p=source_period,
            m=mh,
            d=observed,
            **bounds,
        )
        if old:
            path.unlink()
            result = {
                "import_id": str(old["id"]),
                "state": old["state"],
                "duplicate": True,
            }
            await execute(
                c.conn,
                "INSERT INTO ps_requests(tenant_id,request_key,request_hash,result) VALUES(:t,:k,:h,CAST(:r AS jsonb))",
                t=c.tenant,
                k=request_key,
                h=upload_hash,
                r=js({"body": result, "status": 202}),
            )
            return result
        await execute(
            c.conn,
            "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,source_period,observed_on,mapping_hash,state,created_by,source_type,period_type,period_start,period_end) VALUES(:t,:id,:n,:h,:k,:s,:p,:d,:m,'uploaded',:u,:source,:kind,:start,:end)",
            t=c.tenant,
            id=id,
            n=name,
            h=sha,
            k=key,
            s=count,
            p=source_period,
            d=observed,
            m=mh,
            **bounds,
            u=c.user,
        )
        job = await new_job(
            c, "parse", {"import_id": id, "generation": 1}, "parse:" + id + ":1"
        )
        result = {"import_id": id, **job}
        await execute(
            c.conn,
            "INSERT INTO ps_requests(tenant_id,request_key,request_hash,result) VALUES(:t,:k,:h,CAST(:r AS jsonb))",
            t=c.tenant,
            k=request_key,
            h=upload_hash,
            r=js({"body": result, "status": 202}),
        )
        return result
    except BaseException:
        path.unlink(missing_ok=True)
        raise
    finally:
        await file.close()


@router.get("/imports")
async def imports(
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    return await paginated(ctx(request), "ps_imports", page=page, page_size=page_size)


@router.get("/imports/{id}")
async def import_detail(id: UUID, request: Request):
    return await resource(ctx(request), "ps_imports", id)


@router.put("/imports/{id}/mapping", status_code=202)
async def mapping(id: UUID, body: C.Mapping, request: Request):
    c = ctx(request)
    p = await resource(c, "ps_imports", id)
    if p["state"] not in ("preview", "failed"):
        fail(409, "当前状态不允许调整映射")
    committed = await one(
        c.conn,
        "SELECT 1 AS n FROM ps_import_rows WHERE tenant_id=:t AND import_id=:id AND state='committed' LIMIT 1",
        t=c.tenant,
        id=str(id),
    )
    if committed:
        fail(
            409, "此批次已有入库行，不能修改来源和统计周期；请重试原任务或新建导入批次"
        )
    generation = p["parse_generation"] + 1
    await execute(
        c.conn,
        "UPDATE ps_imports SET mapping=CAST(:m AS jsonb),mapping_hash=:h,observed_on=:d,source_period=:p,source_type=:source,period_type=:kind,period_start=:start,period_end=:end,state='uploaded',parse_generation=:g WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(id),
        m=js(body.mapping),
        h=digest(body.mapping),
        d=body.observed_on,
        p=body.source_period,
        source=body.source_type,
        kind=body.period_type,
        start=body.period_start,
        end=body.period_end,
        g=generation,
    )
    return await new_job(
        c,
        "parse",
        {"import_id": str(id), "generation": generation},
        f"parse:{id}:{generation}",
    )


@router.get("/imports/{id}/preview")
async def preview_import(
    id: UUID,
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    c = ctx(request)
    await resource(c, "ps_imports", id)
    return await paginated(
        c, "ps_import_rows", "import_id=:i", {"i": str(id)}, page, page_size
    )


@router.post("/imports/{id}/commit", status_code=202)
async def commit_import(id: UUID, request: Request):
    c = ctx(request)
    p = await resource(c, "ps_imports", id)
    if not p["observed_on"] or not p["period_start"] or not p["period_end"]:
        fail(422, "请先填写观察日期与统计周期并重新解析")
    if p["state"] not in ("preview", "importing", "completed", "partial"):
        fail(409, "解析完成后才能确认导入")
    if not p["counts"].get("valid"):
        fail(422, "没有有效商品行")
    await ensure_policy(c)
    return await new_job(
        c,
        "import",
        {"import_id": str(id), "generation": p["parse_generation"]},
        f"import:{id}:{p['parse_generation']}",
    )


@router.get("/imports/{id}/source")
async def source(id: UUID, request: Request):
    p = await resource(ctx(request), "ps_imports", id)
    path = storage() / p["storage_key"]
    if not path.is_file():
        fail(404, "源文件不存在")
    return FileResponse(path, filename=p["file_name"])


@router.get("/imports/{id}/errors")
async def import_errors(id: UUID, request: Request):
    import csv, io
    from .domain import excel_text

    c = ctx(request)
    await resource(c, "ps_imports", id)
    errors = await rows(
        c.conn,
        "SELECT member_path,sheet_name,row_no,errors FROM ps_import_rows WHERE tenant_id=:t AND import_id=:id AND state='invalid' ORDER BY member_path,sheet_name,row_no",
        t=c.tenant,
        id=str(id),
    )
    out = io.StringIO()
    writer = csv.writer(out)
    writer.writerow(["文件", "工作表", "行号", "错误"])
    for row in errors:
        writer.writerow(
            [
                excel_text(row["member_path"]),
                excel_text(row["sheet_name"]),
                row["row_no"],
                excel_text("; ".join(row["errors"])),
            ]
        )
    return Response(
        out.getvalue().encode("utf-8-sig"),
        media_type="text/csv",
        headers={"Content-Disposition": 'attachment; filename="import-errors.csv"'},
    )


@router.get("/policies")
async def policies(request: Request):
    c = ctx(request)
    return {
        "items": await rows(
            c.conn,
            "SELECT * FROM ps_policies WHERE tenant_id=:t ORDER BY version DESC",
            t=c.tenant,
        ),
        "default_config": json.loads(
            (Path(__file__).parent / "default-policy.json").read_text()
        ),
    }


@router.post("/policies", status_code=201)
async def policy_create(body: C.Policy, request: Request):
    c = ctx(request)
    owner(c)
    id = uid()
    await execute(
        c.conn,
        "INSERT INTO ps_policies(tenant_id,id,version,name,config,config_hash,created_by) SELECT :t,:id,COALESCE(max(version),0)+1,:n,CAST(:c AS jsonb),:h,:u FROM ps_policies WHERE tenant_id=:t",
        t=c.tenant,
        id=id,
        n=body.name,
        c=js(body.config),
        h=digest(body.config),
        u=c.user,
    )
    return await resource(c, "ps_policies", id)


@router.post("/policies/{id}/test")
async def policy_test(id: UUID, body: C.Selection, request: Request):
    c = ctx(request)
    p = await resource(c, "ps_policies", id)
    targets = await selected(c, body)
    if len(targets) > 100:
        fail(422, "试运行最多100个商品，请明确勾选")
    return {
        "items": [
            {
                "product_id": str(v["id"]),
                **classify(await resource(c, "ps_products", v["id"]), p["config"]),
            }
            for v in targets
        ]
    }


async def screen_job(c, selection, key, policy=None):
    existing = await one(
        c.conn,
        "SELECT j.id,j.state,r.id AS run_id FROM ps_jobs j LEFT JOIN ps_screen_runs r ON r.tenant_id=j.tenant_id AND r.job_id=j.id WHERE j.tenant_id=:t AND j.kind='screen' AND j.idempotency_key=:k",
        t=c.tenant,
        k=key,
    )
    if existing:
        return {
            "job_id": str(existing["id"]),
            "state": existing["state"],
            "run_id": str(existing["run_id"]),
        }
    policy = policy or await ensure_policy(c)
    targets = await selected(c, selection)
    ids = [str(v["id"]) for v in targets]
    await execute(
        c.conn,
        "UPDATE ps_products SET screen_generation=screen_generation+1 WHERE tenant_id=:t AND id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb)))",
        t=c.tenant,
        ids=js(ids),
    )
    snapshots = await rows(
        c.conn,
        "SELECT id,data_revision,screen_generation FROM ps_products WHERE tenant_id=:t AND id IN (SELECT value::uuid FROM jsonb_array_elements_text(CAST(:ids AS jsonb)))",
        t=c.tenant,
        ids=js(ids),
    )
    job = await new_job(
        c,
        "screen",
        {"policy_id": str(policy["id"]), "targets": jsonable_encoder(snapshots)},
        key,
    )
    run = uid()
    await execute(
        c.conn,
        "INSERT INTO ps_screen_runs(tenant_id,id,job_id,policy_id) VALUES(:t,:id,:j,:p)",
        t=c.tenant,
        id=run,
        j=job["job_id"],
        p=str(policy["id"]),
    )
    return {**job, "run_id": run}


@router.post("/policies/{id}/activate", status_code=202)
async def activate(id: UUID, request: Request):
    c = ctx(request)
    owner(c)
    p = await resource(c, "ps_policies", id)
    await execute(
        c.conn,
        "UPDATE ps_policies SET active=false WHERE tenant_id=:t AND active",
        t=c.tenant,
    )
    await execute(
        c.conn,
        "UPDATE ps_policies SET active=true WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(id),
    )
    return await screen_job(c, C.Selection(), request.headers["idempotency-key"], p)


@router.post("/screen-runs", status_code=202)
async def rescreen(body: C.Selection, request: Request):
    return await screen_job(ctx(request), body, request.headers["idempotency-key"])


@router.get("/screen-runs/{id}")
async def run_detail(id: UUID, request: Request):
    return await resource(ctx(request), "ps_screen_runs", id)


@router.post("/review-batches/preview", status_code=201)
async def preview_batch(body: C.Batch, request: Request):
    c = ctx(request)
    reference = None
    status = body.status
    reason = body.reason
    note = body.note
    if body.mode == "reference":
        reference = await resource(c, "ps_products", body.reference_product_id)
        if reference["review_status"] == "pending":
            fail(422, "参考样本尚未复核")
        status = reference["review_status"]
        reason = reference["review_reason"]
        note = reference["review_note"]
    targets = []
    for id in body.product_ids:
        p = await resource(c, "ps_products", id)
        targets.append(
            {
                "id": str(id),
                "asin": p["asin"],
                "title": p["title"],
                "data_revision": p["data_revision"],
                "review_revision": p["review_revision"],
                "eligible": p["review_status"] == "pending",
                "skip_reason": ""
                if p["review_status"] == "pending"
                else "已有人工结论，未覆盖",
            }
        )
    payload = {
        "targets": targets,
        "mode": body.mode,
        "status": status,
        "reason": reason,
        "note": note,
        "reference": None
        if reference is None
        else {
            k: jsonable_encoder(reference[k])
            for k in ("id", "data_revision", "review_revision")
        },
    }
    id = uid()
    expires = datetime.now(timezone.utc) + timedelta(minutes=15)
    await execute(
        c.conn,
        "INSERT INTO ps_review_batches(tenant_id,id,idempotency_key,state,payload,expires_at,created_by) VALUES(:t,:id,:k,'preview',CAST(:p AS jsonb),:e,:u)",
        t=c.tenant,
        id=id,
        k=request.headers["idempotency-key"],
        p=js(payload),
        e=expires,
        u=c.user,
    )
    return {"id": id, "expires_at": expires, **payload}


@router.get("/review-batches")
async def batches(
    request: Request,
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=100),
):
    return await paginated(
        ctx(request), "ps_review_batches", page=page, page_size=page_size
    )


@router.get("/review-batches/{id}")
async def batch_detail(id: UUID, request: Request):
    return await resource(ctx(request), "ps_review_batches", id)


@router.post("/review-batches/{id}/apply")
async def apply_batch(id: UUID, request: Request):
    c = ctx(request)
    batch = await resource(c, "ps_review_batches", id)
    if batch["state"] in ("applied", "undone"):
        return batch["result"]
    if batch["expires_at"] <= datetime.now(timezone.utc):
        fail(409, "预览已过期，请重新预览")
    payload = batch["payload"]
    ref = payload["reference"]
    if ref:
        p = await resource(c, "ps_products", ref["id"])
        if (
            p["data_revision"] != ref["data_revision"]
            or p["review_revision"] != ref["review_revision"]
        ):
            fail(409, "参考样本已变化，请重新预览")
    items = []
    for target in payload["targets"]:
        p = await resource(c, "ps_products", target["id"])
        ok = (
            target["eligible"]
            and p["review_status"] == "pending"
            and p["review_revision"] == target["review_revision"]
            and p["data_revision"] == target["data_revision"]
        )
        if ok:
            await review_write(
                c, p, payload["status"], payload["reason"], payload["note"], str(id)
            )
        items.append(
            {
                "id": target["id"],
                "asin": p["asin"],
                "success": ok,
                "reason": "" if ok else target["skip_reason"] or "预览后已变化，未覆盖",
            }
        )
    result = {
        "id": str(id),
        "items": items,
        "success": sum(x["success"] for x in items),
        "skipped": sum(not x["success"] for x in items),
    }
    await execute(
        c.conn,
        "UPDATE ps_review_batches SET state='applied',result=CAST(:r AS jsonb) WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(id),
        r=js(result),
    )
    return result


@router.post("/review-batches/{id}/undo")
async def undo_batch(id: UUID, request: Request):
    c = ctx(request)
    batch = await resource(c, "ps_review_batches", id)
    if batch["state"] == "undone":
        return batch["undo_result"]
    if batch["state"] != "applied":
        fail(409, "批次尚未执行")
    events = await rows(
        c.conn,
        "SELECT * FROM ps_review_events WHERE tenant_id=:t AND batch_id=:id AND undo_of IS NULL ORDER BY product_id",
        t=c.tenant,
        id=str(id),
    )
    items = []
    for event in events:
        p = await resource(c, "ps_products", event["product_id"])
        ok = p["review_revision"] == event["after_revision"]
        before = event["before_value"]
        if ok:
            await review_write(
                c,
                p,
                before["review_status"],
                before["review_reason"],
                before["review_note"],
                str(id),
                str(event["id"]),
            )
        items.append(
            {
                "id": str(p["id"]),
                "success": ok,
                "reason": "" if ok else "之后已有修改，未覆盖",
            }
        )
    result = {
        "id": str(id),
        "items": items,
        "success": sum(x["success"] for x in items),
        "skipped": sum(not x["success"] for x in items),
    }
    await execute(
        c.conn,
        "UPDATE ps_review_batches SET state='undone',undo_result=CAST(:r AS jsonb) WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(id),
        r=js(result),
    )
    return result


async def profit_product(c, id, body):
    p = await product_with_metrics(c.conn, c.tenant, id, body.monthly_id)
    if not p:
        fail(404, "未找到商品")
    if body.monthly_id is not None and p["monthly_id"] is None:
        fail(404, "未找到此商品的月度记录")
    if (
        body.expected_monthly_revision is not None
        and p["monthly_revision"] != body.expected_monthly_revision
    ):
        fail(409, "月度数据已更新，请重新测算")
    return p


@router.post("/profit/calculate")
async def profit_calculate(body: C.ProfitRequest, request: Request):
    p = await profit_product(ctx(request), body.product_id, body)
    if p["data_revision"] != body.expected_data_revision:
        fail(409, "商品已更新")
    return {
        "items": [
            {"inputs": x.model_dump(), "output": calculate(x.model_dump())}
            for x in body.scenarios
        ]
    }


@router.get("/profit/templates")
async def templates(request: Request):
    c = ctx(request)
    return {
        "items": await rows(
            c.conn,
            "SELECT * FROM ps_profit_templates WHERE tenant_id=:t ORDER BY created_at DESC,id DESC",
            t=c.tenant,
        )
    }


@router.post("/profit/templates", status_code=201)
async def template_create(body: C.Template, request: Request):
    c = ctx(request)
    owner(c)
    id = uid()
    await execute(
        c.conn,
        "INSERT INTO ps_profit_templates(tenant_id,id,name,version,params,created_by) SELECT :t,:id,:n,COALESCE(max(version),0)+1,CAST(:p AS jsonb),:u FROM ps_profit_templates WHERE tenant_id=:t AND name=:n",
        t=c.tenant,
        id=id,
        n=body.name,
        p=js(body.params.model_dump()),
        u=c.user,
    )
    return await resource(c, "ps_profit_templates", id)


@router.get("/products/{id}/profit-scenarios")
async def scenarios(id: UUID, request: Request):
    c = ctx(request)
    p = await resource(c, "ps_products", id)
    items = await rows(
        c.conn,
        "SELECT q.*, (product_data_revision<>:v OR (monthly_id IS NOT NULL AND monthly_revision IS DISTINCT FROM (SELECT m.revision FROM ps_product_monthly m WHERE m.tenant_id=q.tenant_id AND m.id=q.monthly_id)) OR EXISTS(SELECT 1 FROM ps_profit_templates newer JOIN ps_profit_templates old ON old.tenant_id=newer.tenant_id AND old.name=newer.name WHERE old.tenant_id=q.tenant_id AND old.id=q.template_id AND newer.version>old.version)) AS stale FROM ps_profit_scenarios q WHERE tenant_id=:t AND product_id=:id ORDER BY created_at DESC,id DESC",
        t=c.tenant,
        id=str(id),
        v=p["data_revision"],
    )
    return {"items": items}


async def save_scenario(c, p, body):
    p = await profit_product(c, p["id"], body)
    if p["data_revision"] != body.expected_data_revision:
        fail(409, "商品已更新")
    template = (
        await resource(c, "ps_profit_templates", body.template_id)
        if body.template_id
        else None
    )
    inputs = body.inputs.model_dump()
    output = calculate(inputs)
    id = uid()
    await execute(
        c.conn,
        "INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,template_id,name,inputs,output,template_snapshot,formula_version,product_data_revision,created_by,monthly_id,monthly_revision,monthly_snapshot) VALUES(:t,:id,:p,:template,:name,CAST(:i AS jsonb),CAST(:o AS jsonb),CAST(:snapshot AS jsonb),:f,:v,:u,:monthly,:monthly_rev,CAST(:monthly_snapshot AS jsonb))",
        t=c.tenant,
        id=id,
        p=str(p["id"]),
        template=str(body.template_id) if body.template_id else None,
        name=inputs["name"],
        i=js(inputs),
        o=js(output),
        snapshot=js(template) if template else None,
        f=FORMULA_VERSION,
        monthly=str(p["monthly_id"]) if p["monthly_id"] else None,
        monthly_rev=p["monthly_revision"],
        monthly_snapshot=js(
            {
                k: p[k]
                for k in (
                    "monthly_id",
                    "monthly_revision",
                    "stat_month",
                    "period_type",
                    "period_start",
                    "period_end",
                    "source_type",
                    "monthly_source_row_id",
                    "price_usd",
                    "monthly_sales",
                )
            }
        )
        if p["monthly_id"]
        else None,
        v=p["data_revision"],
        u=c.user,
    )
    return {"id": id, "inputs": inputs, "output": output}


@router.post("/products/{id}/profit-scenarios", status_code=201)
async def scenario_save(id: UUID, body: C.SaveProfit, request: Request):
    c = ctx(request)
    return await save_scenario(c, await resource(c, "ps_products", id), body)


@router.post("/profit/batches", status_code=202)
async def profit_batch(body: C.ProfitBatch, request: Request):
    c = ctx(request)
    if body.template_id:
        await resource(c, "ps_profit_templates", body.template_id)
    targets = await selected(c, body)
    snapshots = jsonable_encoder(targets)
    return await new_job(
        c,
        "profit_batch",
        {
            "targets": snapshots,
            "inputs": body.inputs.model_dump(),
            "template_id": str(body.template_id) if body.template_id else None,
        },
        request.headers["idempotency-key"],
    )


@router.post("/exports", status_code=202)
async def export_create(body: C.Selection, request: Request):
    c = ctx(request)
    targets = await selected(c, body)
    return await new_job(
        c,
        "export",
        {"product_ids": [str(v["id"]) for v in targets], "filters": body.filters},
        request.headers["idempotency-key"],
    )


@router.get("/jobs/{id}")
async def job_detail(id: UUID, request: Request):
    p = await resource(ctx(request), "ps_jobs", id)
    return {k: v for k, v in p.items() if k not in ("payload", "lease_token")}


@router.post("/jobs/{id}/retry", status_code=202)
async def retry_job(id: UUID, request: Request):
    c = ctx(request)
    p = await resource(c, "ps_jobs", id)
    if p["state"] not in ("failed", "partial"):
        fail(409, "只能重试失败或部分完成的任务")
    if p["kind"] == "import":
        await execute(
            c.conn,
            "UPDATE ps_imports SET state='importing' WHERE tenant_id=:t AND id=:id",
            t=c.tenant,
            id=p["payload"]["import_id"],
        )
    await execute(
        c.conn,
        "UPDATE ps_jobs SET state='queued',error=NULL,lease_until=NULL,lease_token=NULL WHERE tenant_id=:t AND id=:id",
        t=c.tenant,
        id=str(id),
    )
    return {"job_id": str(id), "state": "queued"}


@router.get("/exports/{job_id}/download")
async def export_download(job_id: UUID, request: Request):
    p = await resource(ctx(request), "ps_jobs", job_id)
    if p["kind"] != "export" or p["state"] != "succeeded":
        fail(409, "导出尚未完成")
    path = storage() / p["result"]["storage_key"]
    if not path.is_file():
        fail(404, "导出文件不存在")
    return FileResponse(path, filename="product-screening.xlsx")
