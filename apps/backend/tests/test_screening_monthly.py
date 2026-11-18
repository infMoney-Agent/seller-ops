import asyncio
import hashlib
import hmac
import io
import os
import time
from datetime import date
from uuid import uuid4
import pytest
from app.screening.monthly import period_metadata


def test_period_boundaries():
    assert period_metadata(date(2024, 3, 5), "2024-02")["period_end"] == date(
        2024, 2, 29
    )
    assert period_metadata(date(2026, 10, 31), "", "rolling_30d")[
        "period_start"
    ] == date(2026, 10, 2)
    for args in [
        dict(source_period="2026-13"),
        dict(period_type="custom"),
        dict(
            period_type="rolling_30d",
            period_start=date(2026, 10, 1),
            period_end=date(2026, 10, 31),
        ),
        dict(
            source_period="2026-10",
            period_start=date(2026, 11, 1),
            period_end=date(2026, 11, 30),
        ),
    ]:
        with pytest.raises(ValueError):
            period_metadata(date(2026, 10, 31), **args)


def test_monthly_history_and_profit_versions(tmp_path, monkeypatch):
    url = os.environ.get("SCREENING_TEST_DATABASE_URL")
    if not url:
        pytest.skip("Requires isolated migrated PostgreSQL")
    monkeypatch.setenv("SCREENING_DATABASE_URL", url)
    monkeypatch.setenv("SCREENING_STORAGE_DIR", str(tmp_path))
    monkeypatch.setenv(
        "SCREENING_INTERNAL_SECRET", "monthly-test-secret-with-at-least-32-characters"
    )
    asyncio.run(monthly_workflow())


async def monthly_workflow():
    import httpx
    from fastapi import FastAPI
    from app.screening.api import router
    from app.screening.tasks import run_job
    from app.screening import db
    from tests.test_screening import profit

    app = FastAPI()
    app.include_router(router, prefix="/api/v1")
    tenant = int(time.time() * 1000) % 1000000000 + 1000000000
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://test"
    ) as client:

        async def call(method, path, body=None, scope=None, **kwargs):
            scope = scope or tenant
            target = "/api/v1/screening" + path
            stamp = str(int(time.time()))
            signature = hmac.new(
                os.environ["SCREENING_INTERNAL_SECRET"].encode(),
                "\n".join([stamp, method, target, "1", str(scope), "owner"]).encode(),
                hashlib.sha256,
            ).hexdigest()
            response = await client.request(
                method,
                target,
                json=body,
                headers={
                    "x-seller-user": "1",
                    "x-seller-workspace": str(scope),
                    "x-seller-role": "owner",
                    "x-screening-time": stamp,
                    "x-screening-signature": signature,
                    "idempotency-key": str(uuid4()),
                },
                **kwargs,
            )
            assert response.status_code < 500, response.text
            return response

        async def import_period(
            month, observed, price, sales, source="sellersprite", kind="calendar_month"
        ):
            data = f"ASIN,Title,Price,Monthly Sales,Monthly Revenue,Rating,Review Count,BSR Rank\nMONTHLY001,Manual tray,{price},{sales},{int(price) * int(sales)},4.5,30,100\n"
            upload = await call(
                "POST",
                "/imports",
                files={"file": ("monthly.csv", data, "text/csv")},
                data={
                    "source_period": month,
                    "observed_on": observed,
                    "source_type": source,
                    "period_type": kind,
                },
            )
            assert upload.status_code == 202, upload.text
            item = upload.json()
            if item.get("duplicate"):
                return item
            await run_job(tenant, item["job_id"])
            commit = await call("POST", f"/imports/{item['import_id']}/commit")
            assert commit.status_code == 202, commit.text
            await run_job(tenant, commit.json()["job_id"])
            job = (await call("GET", "/jobs/" + commit.json()["job_id"])).json()
            assert job["state"] == "succeeded", job
            await run_job(tenant, job["result"]["screen"]["job_id"])
            return item

        await import_period("2026-10", "2026-10-31", 20, 100)
        await import_period("2026-11", "2026-12-05", 22, 200)
        listing = (await call("GET", "/products")).json()
        assert listing["total"] == 1
        p = listing["items"][0]
        id = p["id"]
        assert p["price_usd"].startswith("22")
        review = await call(
            "PATCH",
            f"/products/{id}/review",
            {
                "expected_data_revision": p["data_revision"],
                "expected_review_revision": p["review_revision"],
                "status": "kept",
                "reason": "保留手动工具",
                "note": "人工备注",
            },
        )
        assert review.status_code == 200, review.text
        await import_period("2026-09", "2026-09-30", 15, 90)
        octp = (await call("GET", "/products?stat_month=2026-10")).json()["items"][0]
        assert octp["price_usd"].startswith("20") and octp["monthly_sales"] == 100
        assert octp["review_status"] == "kept" and octp["review_note"] == "人工备注"
        saved = await call(
            "POST",
            f"/products/{id}/profit-scenarios",
            {
                "expected_data_revision": octp["data_revision"],
                "monthly_id": octp["monthly_id"],
                "expected_monthly_revision": octp["monthly_revision"],
                "inputs": profit(),
            },
        )
        assert saved.status_code == 201, saved.text
        revision = p["data_revision"]
        await import_period("2026-10", "2026-11-20", 21, 110)
        now = (await call("GET", f"/products/{id}")).json()
        assert now["price_usd"].startswith("22") and now["data_revision"] == revision
        rows = (await call("GET", f"/products/{id}/monthly")).json()
        assert rows["total"] == 3
        october = next(v for v in rows["items"] if v["stat_month"] == "2026-10-01")
        assert october["revision"] == 2 and october["monthly_sales"] == 110
        assert (
            await call(
                "POST",
                "/profit/calculate",
                {
                    "product_id": id,
                    "expected_data_revision": now["data_revision"],
                    "monthly_id": octp["monthly_id"],
                    "expected_monthly_revision": 1,
                    "scenarios": [profit()],
                },
            )
        ).status_code == 409
        scenarios = (await call("GET", f"/products/{id}/profit-scenarios")).json()[
            "items"
        ]
        assert scenarios[0]["stale"] is True and scenarios[0]["monthly_snapshot"][
            "price_usd"
        ].startswith("20")
        # Same month with another source or another period is a separate observation.
        await import_period("2026-10", "2026-10-31", 9, 80, kind="rolling_30d")
        await import_period("2026-10", "2026-10-31", 17, 99, source="manual")
        assert (await call("GET", f"/products/{id}/monthly")).json()["total"] == 5
        manual = (
            await call("GET", "/products?stat_month=2026-10&source_type=manual")
        ).json()["items"][0]
        assert manual["price_usd"].startswith("17")
        rolling = (
            await call("GET", "/products?stat_month=2026-10&period_type=rolling_30d")
        ).json()["items"][0]
        assert (
            rolling["price_usd"].startswith("9")
            and rolling["period_start"] == "2026-10-02"
        )
        assert len((await call("GET", "/months")).json()["items"]) == 5
        assert (
            await call("GET", f"/products/{id}/monthly", scope=tenant + 1)
        ).status_code == 404
        assert (await call("GET", "/products?stat_month=2026-13")).status_code == 422
        # A batch uses the chosen historical month and freezes its exact monthly version.
        batch = (
            await call(
                "POST",
                "/profit/batches",
                {
                    "product_ids": [id],
                    "filters": {"stat_month": "2026-10", "source_type": "manual"},
                    "inputs": profit(sale_price_usd=None),
                },
            )
        ).json()
        await run_job(tenant, batch["job_id"])
        result = (await call("GET", "/jobs/" + batch["job_id"])).json()
        assert result["state"] == "succeeded", result
        scenarios = (await call("GET", f"/products/{id}/profit-scenarios")).json()[
            "items"
        ]
        assert scenarios[0]["monthly_id"] == manual["monthly_id"] and scenarios[0][
            "inputs"
        ]["sale_price_usd"].startswith("17")
        export = (
            await call(
                "POST",
                "/exports",
                {"filters": {"stat_month": "2026-10", "source_type": "manual"}},
            )
        ).json()
        await run_job(tenant, export["job_id"])
        document = await call("GET", f"/exports/{export['job_id']}/download")
        from openpyxl import load_workbook

        book = load_workbook(io.BytesIO(document.content))
        assert book["月度数据"].max_row == 6
        async with db.transaction() as conn:
            columns = await db.rows(
                conn,
                "SELECT column_name FROM information_schema.columns WHERE table_name='ps_products'",
            )
            assert not {"price_usd", "monthly_sales", "review_count", "rating"} & {
                v["column_name"] for v in columns
            }
    await db.engine().dispose()
    db._engine = None
