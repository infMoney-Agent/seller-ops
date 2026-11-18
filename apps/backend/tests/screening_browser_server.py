"""Loopback-only fixture server for the production-built Web E2E test."""

import asyncio
import os
from fastapi import FastAPI
from app.screening.api import router
from app.screening import db
from app.screening.tasks import run_job

app = FastAPI()
app.include_router(router, prefix="/api/v1")
session = {
    "id": 1,
    "name": "初筛测试",
    "email": "screening@example.test",
    "workspaceId": 1800000001,
    "role": "owner",
    "workspaces": [{"id": 1800000001, "name": "隔离测试工作区", "role": "owner"}],
}
session["workspaceId"] = int(os.environ.get("SCREENING_BROWSER_TENANT", "1800000001"))
session["workspaces"][0]["id"] = session["workspaceId"]


@app.get("/api/auth/session")
def auth():
    return session


async def pump():
    while True:
        async with db.transaction() as conn:
            jobs = await db.rows(
                conn,
                "SELECT id FROM ps_jobs WHERE tenant_id=:t AND state='queued' ORDER BY created_at LIMIT 5",
                t=session["workspaceId"],
            )
        for job in jobs:
            await run_job(session["workspaceId"], str(job["id"]))
        await asyncio.sleep(0.2)


@app.on_event("startup")
async def start():
    app.state.pump = asyncio.create_task(pump())


@app.on_event("shutdown")
async def stop():
    app.state.pump.cancel()
