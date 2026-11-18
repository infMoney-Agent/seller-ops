"""Verify 0002 -> 0003 on synthetic legacy data in a dedicated *_migration_test DB."""

import asyncio
import json
import os
import re
from urllib.parse import urlsplit, urlunsplit
from uuid import uuid4
import asyncpg
from alembic import command
from alembic.config import Config

url = os.environ["SCREENING_MIGRATION_TEST_DATABASE_URL"]
dsn = url.replace("postgresql+asyncpg://", "postgresql://", 1)
parts = urlsplit(dsn)
database = parts.path.lstrip("/")
if not re.fullmatch(r"[a-z0-9_]+_migration_test", database):
    raise RuntimeError("Dedicated *_migration_test database required")
os.environ["SCREENING_DATABASE_URL"] = url
product = str(uuid4())
profit = str(uuid4())
tenant = 92001


async def create():
    admin = await asyncpg.connect(urlunsplit(parts._replace(path="/postgres")))
    try:
        if await admin.fetchval("SELECT 1 FROM pg_database WHERE datname=$1", database):
            raise RuntimeError(
                "Use a fresh migration test database; existing databases are never cleared"
            )
        await admin.execute('CREATE DATABASE "' + database + '"')
    finally:
        await admin.close()


async def seed():
    conn = await asyncpg.connect(dsn)
    try:
        async with conn.transaction():
            await conn.execute(
                "INSERT INTO ps_products(tenant_id,id,asin,title,price_usd,monthly_sales,review_count,rating,review_status,review_reason,review_note,data_revision,review_revision) VALUES($1,$2,'MIGRATE001','Manual tray',22,200,30,4.5,'kept','保留原因','不得覆盖人工备注',7,1)",
                tenant,
                product,
            )
            latest = None
            for month, price, sales in [
                ("2026-09", 15, 90),
                ("2026-10", 20, 100),
                ("2026-11", 22, 200),
            ]:
                import_id, row = str(uuid4()), str(uuid4())
                latest = row
                from datetime import date

                observed = date.fromisoformat(month + "-28")
                await conn.execute(
                    "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,source_period,observed_on,mapping_hash,state,created_by) VALUES($1,$2,$3,$4,'test',0,$3,$5,repeat('0',64),'completed',1)",
                    tenant,
                    import_id,
                    month,
                    month.replace("-", "").ljust(64, "0"),
                    observed,
                )
                normalized = json.dumps(
                    {
                        "asin": "MIGRATE001",
                        "title": "Manual tray",
                        "price_usd": str(price),
                        "monthly_sales": sales,
                        "review_count": 30,
                        "rating": "4.5",
                    }
                )
                await conn.execute(
                    "INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data,normalized_data) VALUES($1,$2,$3,2,$4,'committed',$5::jsonb,$5::jsonb)",
                    tenant,
                    row,
                    import_id,
                    product,
                    normalized,
                )
            await conn.execute(
                "UPDATE ps_products SET current_row_id=$3 WHERE tenant_id=$1 AND id=$2",
                tenant,
                product,
                latest,
            )
            await conn.execute(
                'INSERT INTO ps_review_events(tenant_id,id,product_id,actor_id,before_revision,after_revision,before_value,after_value) VALUES($1,$2,$3,1,0,1,\'{"review_status":"pending"}\',\'{"review_status":"kept","review_note":"不得覆盖人工备注"}\')',
                tenant,
                str(uuid4()),
                product,
            )
            await conn.execute(
                "INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,name,inputs,output,formula_version,product_data_revision,created_by) VALUES($1,$2,$3,'旧方案','{\"sale_price_usd\":\"22\"}','{\"profit_usd\":\"7.80\"}','fba-us-v1',7,1)",
                tenant,
                profit,
                product,
            )
    finally:
        await conn.close()


async def verify():
    conn = await asyncpg.connect(dsn)
    try:
        rows = await conn.fetch(
            "SELECT stat_month,price_usd,monthly_sales,source_type FROM ps_product_monthly WHERE tenant_id=$1 ORDER BY stat_month",
            tenant,
        )
        assert len(rows) == 3 and [r["monthly_sales"] for r in rows] == [
            90,
            100,
            200,
        ], rows
        assert all(r["source_type"] == "legacy_import" for r in rows)
        p = await conn.fetchrow(
            "SELECT p.*,m.price_usd,m.monthly_sales FROM ps_products p JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id WHERE p.tenant_id=$1 AND p.id=$2",
            tenant,
            product,
        )
        assert p["price_usd"] == 22 and p["monthly_sales"] == 200
        assert (
            p["review_status"] == "kept"
            and p["review_note"] == "不得覆盖人工备注"
            and p["review_revision"] == 1
        )
        assert (
            await conn.fetchval(
                "SELECT count(*) FROM ps_import_rows WHERE tenant_id=$1", tenant
            )
            == 3
        )
        assert (
            await conn.fetchval(
                "SELECT count(*) FROM ps_review_events WHERE tenant_id=$1", tenant
            )
            == 1
        )
        q = await conn.fetchrow(
            "SELECT * FROM ps_profit_scenarios WHERE tenant_id=$1 AND id=$2",
            tenant,
            profit,
        )
        assert (
            json.loads(q["output"])["profit_usd"] == "7.80"
            and q["monthly_id"] == p["current_monthly_id"]
        )
        assert not await conn.fetchval(
            "SELECT count(*) FROM information_schema.columns WHERE table_name='ps_products' AND column_name IN ('price_usd','monthly_sales','rating','review_count')"
        )
        print(
            "Migration 0002 -> 0003 passed: three months recovered; source rows, manual feedback and profit snapshots preserved"
        )
    finally:
        await conn.close()


asyncio.run(create())
config = Config("alembic-screening.ini")
command.upgrade(config, "screening_0002")
asyncio.run(seed())
command.upgrade(config, "head")
asyncio.run(verify())
