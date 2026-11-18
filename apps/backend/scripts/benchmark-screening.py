"""Reproducible benchmark against an explicitly isolated, migrated test database."""

import asyncio
import hashlib
import hmac
import json
import os
import statistics
import time
from uuid import uuid4
from fastapi import FastAPI
import httpx
from app.screening import db
from app.screening.api import router


async def main():
    target = os.environ["SCREENING_TEST_DATABASE_URL"]
    os.environ["SCREENING_DATABASE_URL"] = target
    os.environ["SCREENING_INTERNAL_SECRET"] = (
        "benchmark-only-secret-with-at-least-32-characters"
    )
    tenant = 1900000001
    async with db.transaction(tenant) as conn:
        exists = await db.one(
            conn, "SELECT count(*) AS n FROM ps_products WHERE tenant_id=:t", t=tenant
        )
        if not exists["n"]:
            id = str(uuid4())
            await db.execute(
                conn,
                "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on) VALUES(:t,:id,'benchmark.csv',repeat('0',64),'test',0,repeat('0',64),'completed',1,'2026-01','2026-01-31')",
                t=tenant,
                id=id,
            )
            await db.execute(
                conn,
                "INSERT INTO ps_products(tenant_id,id,asin,title,category,seller_region) SELECT :t,md5('bench-product-'||i)::uuid,lpad(i::text,10,'0'),'Manual steel tray '||i,'Trays','CN' FROM generate_series(1,100000) i",
                t=tenant,
            )
            await db.execute(
                conn,
                "INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data,normalized_data) SELECT :t,md5('bench-row-'||i)::uuid,:id,i,md5('bench-product-'||(((i-1)%100000)+1))::uuid,'committed','{}','{}' FROM generate_series(1,500000) i",
                t=tenant,
                id=id,
            )
            await db.execute(
                conn,
                "UPDATE ps_products SET current_row_id=md5('bench-row-'||asin::int)::uuid WHERE tenant_id=:t",
                t=tenant,
            )
            await db.execute(
                conn,
                "INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,price_usd,monthly_sales) SELECT :t,md5('bench-month-'||asin)::uuid,id,'2026-01-01','calendar_month','2026-01-01','2026-01-31','2026-01-31','benchmark',current_row_id,20,asin::int%10000 FROM ps_products WHERE tenant_id=:t",
                t=tenant,
            )
            await db.execute(
                conn,
                "UPDATE ps_products SET current_monthly_id=md5('bench-month-'||asin)::uuid,current_monthly_revision=1 WHERE tenant_id=:t",
                t=tenant,
            )
            jid, pid, rid = [str(uuid4()) for _ in range(3)]
            await db.execute(
                conn,
                "INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,created_by) VALUES(:t,:id,'screen','benchmark',1)",
                t=tenant,
                id=jid,
            )
            await db.execute(
                conn,
                "INSERT INTO ps_policies(tenant_id,id,version,name,config,config_hash,active,created_by) VALUES(:t,:id,1,'benchmark','{}',repeat('0',64),true,1)",
                t=tenant,
                id=pid,
            )
            await db.execute(
                conn,
                "INSERT INTO ps_screen_runs(tenant_id,id,job_id,policy_id) VALUES(:t,:id,:j,:p)",
                t=tenant,
                id=rid,
                j=jid,
                p=pid,
            )
            await db.execute(
                conn,
                "INSERT INTO ps_screen_results(tenant_id,id,run_id,product_id,source_row_id,data_revision,generation,decision,recommendation,direction,group_key,representative_id,reasons,evidence) SELECT :t,md5('bench-result-'||asin)::uuid,:run,id,current_row_id,0,0,'passed','regular','Trays',md5(asin),id,'[]','[]' FROM ps_products WHERE tenant_id=:t",
                t=tenant,
                run=rid,
            )
            await db.execute(
                conn,
                "UPDATE ps_products SET current_result_id=md5('bench-result-'||asin)::uuid WHERE tenant_id=:t",
                t=tenant,
            )
    async with db.transaction() as conn:
        for table in (
            "ps_products",
            "ps_import_rows",
            "ps_screen_results",
            "ps_profit_scenarios",
        ):
            await db.execute(conn, "ANALYZE " + table)
    app = FastAPI()
    app.include_router(router, prefix="/api/v1")
    report = {}
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://test", timeout=60
    ) as client:
        for suffix in (
            "/products?show_merged=true",
            "/products?review_status=pending",
            "/products?machine=passed",
            "/products?stat_month=2026-01",
            "/products?q=Manual",
            "/summary",
        ):
            timings = []
            for iteration in range(8):
                path = "/api/v1/screening" + suffix
                stamp = str(int(time.time()))
                signature = hmac.new(
                    os.environ["SCREENING_INTERNAL_SECRET"].encode(),
                    "\n".join([stamp, "GET", path, "1", str(tenant), "owner"]).encode(),
                    hashlib.sha256,
                ).hexdigest()
                start = time.perf_counter()
                response = await client.get(
                    path,
                    headers={
                        "x-seller-user": "1",
                        "x-seller-workspace": str(tenant),
                        "x-seller-role": "owner",
                        "x-screening-time": stamp,
                        "x-screening-signature": signature,
                    },
                )
                assert response.status_code == 200, response.text
                if iteration:
                    timings.append(time.perf_counter() - start)
            report[suffix] = {
                "p95_seconds": round(sorted(timings)[-1], 3),
                "median_seconds": round(statistics.median(timings), 3),
            }
    report["fixture"] = {
        "products": 100000,
        "observations": 500000,
        "postgres_version": 14,
        "tenant": tenant,
    }
    print(json.dumps(report, indent=2))
    await db.engine().dispose()


asyncio.run(main())
