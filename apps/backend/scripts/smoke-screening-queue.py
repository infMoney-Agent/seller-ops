"""Run with a dedicated Celery worker, Redis and migrated test PostgreSQL."""

import asyncio
import hashlib
import os
import time
from app.screening import db
from app.screening.api import Context, new_job, storage, uid
from app.screening.domain import digest
from app.screening.tasks import dispatch


async def main():
    if (
        os.environ.get("SCREENING_DATABASE_URL")
        != os.environ["SCREENING_TEST_DATABASE_URL"]
    ):
        raise RuntimeError("Explicit test database must match SCREENING_DATABASE_URL")
    tenant = int(time.time() * 1000) % 1000000000 + 1000000000
    id = uid()
    key = f"{tenant}/sources/{id}.csv"
    path = storage() / key
    path.parent.mkdir(parents=True, exist_ok=True)
    data = b"ASIN,Title,Price\nQUEUE00001,Manual tray,20\n"
    path.write_bytes(data)
    async with db.transaction(tenant) as conn:
        await db.execute(
            conn,
            "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by) VALUES(:t,:id,'queue.csv',:h,:k,:n,:m,'uploaded',1)",
            t=tenant,
            id=id,
            h=hashlib.sha256(data).hexdigest(),
            k=key,
            n=len(data),
            m=digest({}),
        )
        job = await new_job(
            Context(tenant, 1, "owner", conn),
            "parse",
            {"import_id": id, "generation": 1},
            "queue-test:" + id,
        )
    # Recover a committed queued job solely from the PostgreSQL outbox scanner.
    await dispatch()
    for _ in range(80):
        await asyncio.sleep(0.25)
        async with db.transaction() as conn:
            row = await db.one(
                conn,
                "SELECT state,result,error FROM ps_jobs WHERE tenant_id=:t AND id=:id",
                t=tenant,
                id=job["job_id"],
            )
        if row["state"] in ("succeeded", "failed"):
            assert row["state"] == "succeeded", row
            assert row["result"]["valid"] == 1, row
            print("Real Redis/Celery outbox recovery passed:", row["state"])
            break
    else:
        raise AssertionError("Celery job did not complete")
    await db.engine().dispose()


asyncio.run(main())
