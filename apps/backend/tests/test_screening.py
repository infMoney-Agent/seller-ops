"""Domain and isolated PostgreSQL integration tests. Never use application databases."""

import asyncio
import hashlib
import hmac
import io
import json
import os
import time
import zipfile
from uuid import uuid4
import pytest
from app.screening.domain import (
    calculate,
    classify,
    normalize,
    parse_file,
    validate_policy,
    excel_text,
)


def profit(**changes):
    return {
        "name": "基准",
        "sale_price_usd": "20",
        "discount_usd": "0",
        "purchase_cny": "14",
        "packaging_cny": "0",
        "freight_cny": "7",
        "other_landed_cny": "0",
        "fx_cny_per_usd": "7",
        "commission_rate": "0.15",
        "fba_fee_usd": "4",
        "storage_usd": "0",
        "ad_rate": "0.1",
        "return_rate": "0.1",
        "loss_per_return_usd": "2",
        "other_usd": "0",
        "monthly_units": 100,
        **changes,
    }


def policy():
    from pathlib import Path

    return json.loads(
        (Path(__file__).parents[1] / "app/screening/default-policy.json").read_text()
    )


def test_profit_arithmetic_and_boundaries():
    result = calculate(profit())
    assert result["profit_usd"] == "7.80" and result["margin"] == "0.390000"
    assert result["monthly_profit_usd"] == "780.00"
    assert result["break_even_price_usd"] == "9.60"
    assert calculate(profit(purchase_cny=None))["state"] == "incomplete"
    assert calculate(profit(purchase_cny="0", freight_cny="0"))["roi"] is None
    assert (
        calculate(profit(commission_rate="0.6", ad_rate="0.5"))["break_even_price_usd"]
        is None
    )
    for changes in (
        {"fx_cny_per_usd": "0"},
        {"ad_rate": "1.1"},
        {"purchase_cny": "NaN"},
        {"discount_usd": "20"},
    ):
        with pytest.raises(ValueError):
            calculate(profit(**changes))


def test_import_and_workbook_safety():
    records = list(
        parse_file(
            "ASIN,商品标题,价格($)\n b000000001 ,手动工具,12.5\nBAD,,\n".encode(),
            "test.csv",
            {},
        )
    )
    assert records[0]["normalized_data"]["asin"] == "B000000001"
    assert records[1]["errors"]
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w") as archive:
        archive.writestr("nested.zip", b"bad")
    with pytest.raises(ValueError):
        list(parse_file(buffer.getvalue(), "test.zip", {}))
    assert excel_text(" =SUM(A1)") == "' =SUM(A1)"
    out, errors = normalize({"ASIN": "B000000001", "Title": "tool", "Rating": "8"}, {})
    assert errors


@pytest.mark.parametrize(
    "title,decision",
    [
        ("Vacuum storage bags with manual pump", "passed"),
        ("Vacuum storage bags with electric pump", "excluded"),
        ("Vacuum storage bags with pump", "insufficient"),
        ("NO LED folding mirror", "passed"),
        ("Electric rechargeable nail drill", "excluded"),
        ("Replacement toothbrush heads for Oral B", "passed"),
    ],
)
def test_rule_boundaries(title, decision):
    assert (
        classify({"asin": "B000000001", "title": title, "category": "Tools"}, policy())[
            "decision"
        ]
        == decision
    )


def test_policy_does_not_contain_old_human_data():
    cfg = policy()
    assert not cfg.get("overrides") and not cfg.get("exploration_recommendations")
    with pytest.raises(ValueError):
        validate_policy({"overrides": {"ASIN": "keep"}})


def test_postgres_workflow(tmp_path, monkeypatch):
    url = os.environ.get("SCREENING_TEST_DATABASE_URL")
    if not url:
        pytest.skip(
            "Set SCREENING_TEST_DATABASE_URL to an isolated migrated PostgreSQL database"
        )
    monkeypatch.setenv("SCREENING_DATABASE_URL", url)
    monkeypatch.setenv("SCREENING_STORAGE_DIR", str(tmp_path))
    monkeypatch.setenv(
        "SCREENING_INTERNAL_SECRET", "test-screening-secret-with-at-least-32-characters"
    )
    asyncio.run(workflow())


async def workflow():
    import httpx
    from fastapi import FastAPI
    from app.screening.api import router
    from app.screening.tasks import run_job
    from app.screening import db

    app = FastAPI()
    app.include_router(router, prefix="/api/v1")
    tenant = int(time.time() * 1000) % 2000000000
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://test"
    ) as client:

        async def call(method, path, body=None, key=None, scope=None, **kwargs):
            path = "/api/v1/screening" + path
            stamp = str(int(time.time()))
            scope = scope or tenant
            message = "\n".join([stamp, method, path, "1", str(scope), "owner"])
            signature = hmac.new(
                os.environ["SCREENING_INTERNAL_SECRET"].encode(),
                message.encode(),
                hashlib.sha256,
            ).hexdigest()
            headers = {
                "x-seller-user": "1",
                "x-seller-workspace": str(scope),
                "x-seller-role": "owner",
                "x-screening-time": stamp,
                "x-screening-signature": signature,
                "idempotency-key": key or str(uuid4()),
            }
            response = await client.request(
                method, path, json=body, headers=headers, **kwargs
            )
            if response.status_code >= 500:
                raise AssertionError(response.text)
            return response

        async def import_csv(date, price="20", suffix=""):
            data = f"ASIN,Title,Price,Seller ID\nB000000001,Manual steel tool,{price},SELLER0001\nB000000002,Manual plastic tray,10,SELLER0001\n{suffix}"
            response = await call(
                "POST",
                "/imports",
                files={"file": ("test.csv", data, "text/csv")},
                data={"source_period": date[:7], "observed_on": date},
            )
            assert response.status_code == 202, response.text
            value = response.json()
            await run_job(tenant, value["job_id"])
            commit = await call("POST", f"/imports/{value['import_id']}/commit")
            assert commit.status_code == 202, commit.text
            await run_job(tenant, commit.json()["job_id"])
            job = (await call("GET", "/jobs/" + commit.json()["job_id"])).json()
            assert job["state"] in ("succeeded", "partial"), job
            await run_job(tenant, job["result"]["screen"]["job_id"])
            return value

        await import_csv("2026-10-31")
        response = await call("GET", "/products?show_merged=true")
        assert response.status_code == 200, response.text
        products = response.json()["items"]
        assert len(products) == 2
        p = next(p for p in products if p["asin"] == "B000000001")
        assert p["decision"] == "passed", p
        assert (await call("GET", "/summary")).json()["source_rows"] == 2
        assert (await call("GET", "/products", scope=tenant + 1)).json()["total"] == 0
        assert (
            await call("GET", "/products/" + p["id"], scope=tenant + 1)
        ).status_code == 404
        bad = await client.get(
            "/api/v1/screening/products", headers={"x-seller-user": "1"}
        )
        assert bad.status_code == 401
        key = str(uuid4())
        body = {
            "expected_review_revision": p["review_revision"],
            "expected_data_revision": p["data_revision"],
            "status": "unsure",
            "reason": "需核实",
            "note": "备注中的排除不改变结论",
        }
        first = await call("PATCH", f"/products/{p['id']}/review", body, key=key)
        assert first.status_code == 200, first.text
        assert (
            await call("PATCH", f"/products/{p['id']}/review", body, key=key)
        ).json() == first.json()
        assert (
            await call(
                "PATCH",
                f"/products/{p['id']}/review",
                {**body, "note": "different"},
                key=key,
            )
        ).status_code == 409
        assert (
            await call("PATCH", f"/products/{p['id']}/review", body)
        ).status_code == 409
        await import_csv("2026-09-30", "1")
        p = (await call("GET", "/products/" + p["id"])).json()
        assert str(p["price_usd"]).startswith("20") and p["review_status"] == "unsure"
        batch = (
            await call(
                "POST",
                "/review-batches/preview",
                {
                    "product_ids": [x["id"] for x in products],
                    "mode": "direct",
                    "status": "kept",
                    "reason": "手工工具",
                },
            )
        ).json()
        result = await call("POST", f"/review-batches/{batch['id']}/apply")
        assert result.json()["success"] == 1 and result.json()["skipped"] == 1, (
            result.text
        )
        assert (
            await call("POST", f"/review-batches/{batch['id']}/apply")
        ).json() == result.json()
        undone = await call("POST", f"/review-batches/{batch['id']}/undo")
        assert undone.json()["success"] == 1, undone.text
        assert (
            await call("POST", f"/review-batches/{batch['id']}/undo")
        ).json() == undone.json()
        scenario = await call(
            "POST",
            f"/products/{p['id']}/profit-scenarios",
            {"expected_data_revision": p["data_revision"], "inputs": profit()},
        )
        assert scenario.status_code == 201, scenario.text
        assert scenario.json()["output"]["profit_usd"] == "7.80"
        job = (await call("POST", "/exports", {})).json()
        await run_job(tenant, job["job_id"])
        state = (await call("GET", "/jobs/" + job["job_id"])).json()
        assert state["state"] == "succeeded", state
        download = await call("GET", f"/exports/{job['job_id']}/download")
        assert download.status_code == 200 and download.content.startswith(b"PK")
        from openpyxl import load_workbook

        book = load_workbook(io.BytesIO(download.content))
        assert book["产品初筛"].max_row == 3
        assert (await call("GET", "/products?profit_min=0")).status_code == 200
        # A changed reference invalidates the whole preview; unsure remains distinct.
        p2 = (
            await call(
                "GET",
                "/products/" + next(x["id"] for x in products if x["id"] != p["id"]),
            )
        ).json()
        refbatch = (
            await call(
                "POST",
                "/review-batches/preview",
                {
                    "product_ids": [p2["id"]],
                    "mode": "reference",
                    "reference_product_id": p["id"],
                },
            )
        ).json()
        edit = {
            "expected_review_revision": p["review_revision"],
            "expected_data_revision": p["data_revision"],
            "status": "unsure",
            "reason": "修改后的样本原因",
            "note": "",
        }
        assert (
            await call("PATCH", f"/products/{p['id']}/review", edit)
        ).status_code == 200
        assert (
            await call("POST", f"/review-batches/{refbatch['id']}/apply")
        ).status_code == 409
        # Concurrent editors cannot overwrite one another.
        edit2 = {
            "expected_review_revision": p2["review_revision"],
            "expected_data_revision": p2["data_revision"],
            "status": "kept",
            "reason": "共同复核",
            "note": "",
        }
        responses = await asyncio.gather(
            call("PATCH", f"/products/{p2['id']}/review", edit2),
            call("PATCH", f"/products/{p2['id']}/review", edit2),
        )
        assert sorted(r.status_code for r in responses) == [200, 409]
        # A newer run supersedes a queued older run.
        older = (await call("POST", "/screen-runs", {})).json()
        newer = (await call("POST", "/screen-runs", {})).json()
        await run_job(tenant, older["job_id"])
        oldstate = (await call("GET", "/jobs/" + older["job_id"])).json()
        assert oldstate["result"]["superseded"] == 2, oldstate
        await run_job(tenant, newer["job_id"])
        assert (await call("GET", "/jobs/" + newer["job_id"])).json()[
            "state"
        ] == "succeeded"
        # Simulate a worker crash after import commit, before final task acknowledgement.
        latest = await import_csv("2026-11-18", "25")
        async with db.transaction(tenant) as conn:
            imported = await db.one(
                conn,
                "SELECT id FROM ps_jobs WHERE tenant_id=:t AND kind='import' AND payload->>'import_id'=:id",
                t=tenant,
                id=latest["import_id"],
            )
            before = await db.one(
                conn,
                "SELECT count(*) AS n FROM ps_import_rows WHERE tenant_id=:t",
                t=tenant,
            )
            await db.execute(
                conn,
                "UPDATE ps_jobs SET state='queued' WHERE tenant_id=:t AND id=:id",
                t=tenant,
                id=str(imported["id"]),
            )
        await run_job(tenant, str(imported["id"]))
        assert (await call("GET", "/jobs/" + str(imported["id"]))).json()[
            "state"
        ] == "succeeded"
        async with db.transaction(tenant) as conn:
            after = await db.one(
                conn,
                "SELECT count(*) AS n FROM ps_import_rows WHERE tenant_id=:t",
                t=tenant,
            )
            assert before == after
        stale = (await call("GET", f"/products/{p['id']}/profit-scenarios")).json()
        assert stale["items"][0]["stale"] is True
    if db._engine is not None:
        await db._engine.dispose()
        db._engine = None
