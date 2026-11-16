from dataclasses import asdict
import json
from typing import Annotated

from fastapi import APIRouter, Depends, Header, Path, Query
from pydantic import BaseModel, Field
from redis.asyncio import Redis

from app.core.config import get_settings
from app.modules.competitor_monitor import MonitoredASIN
from app.modules.competitor_monitor.storage import save_snapshot

router = APIRouter(prefix="/monitor", tags=["Competitor Monitor"])
ASIN_PATTERN = r"^[A-Za-z0-9]{10}$"
AsinPath = Annotated[str, Path(pattern=ASIN_PATTERN)]


class AddMonitorRequest(BaseModel):
    asin: str = Field(pattern=ASIN_PATTERN)
    platform: str = "amazon"
    label: str = Field(default="", max_length=200)
    check_interval_minutes: int = Field(default=60, ge=60, le=10080)
    alert_rules: dict[str, float] = Field(default_factory=dict)


def namespace(x_seller_user: Annotated[int, Header(gt=0)]) -> str:
    # The private service receives the user ID from the authenticated Next.js proxy.
    return f"seller:{x_seller_user}"


Account = Annotated[str, Depends(namespace)]


@router.post("/add")
async def add_monitor(req: AddMonitorRequest, account: Account):
    asin = req.asin.upper()
    async with Redis.from_url(get_settings().REDIS_URL) as redis:
        await redis.set(f"{account}:monitor:{asin}", req.model_copy(update={"asin": asin}).model_dump_json())
    return {"status": "added", "asin": asin}


@router.delete("/remove/{asin}")
async def remove_monitor(asin: AsinPath, account: Account):
    asin = asin.upper()
    async with Redis.from_url(get_settings().REDIS_URL) as redis:
        deleted = await redis.delete(f"{account}:monitor:{asin}")
    return {"status": "removed" if deleted else "not_found", "asin": asin}


@router.get("/list")
async def list_monitors(account: Account):
    monitors = []
    async with Redis.from_url(get_settings().REDIS_URL) as redis:
        async for key in redis.scan_iter(match=f"{account}:monitor:*"):
            raw = await redis.get(key)
            if raw:
                monitors.append(json.loads(raw))
    return {"monitors": sorted(monitors, key=lambda monitor: monitor["asin"])}


@router.get("/snapshots/{asin}")
async def get_snapshots(asin: AsinPath, account: Account, limit: Annotated[int, Query(ge=1, le=1000)] = 100):
    asin = asin.upper()
    async with Redis.from_url(get_settings().REDIS_URL) as redis:
        data = await redis.lrange(f"{account}:snapshots:{asin}", 0, limit - 1)
    return {"asin": asin, "snapshots": [json.loads(item) for item in data]}


@router.post("/snapshot/{asin}")
async def manual_snapshot(asin: AsinPath, account: Account):
    asin = asin.upper()
    async with Redis.from_url(get_settings().REDIS_URL) as redis:
        raw = await redis.get(f"{account}:monitor:{asin}")
        monitored = MonitoredASIN(**json.loads(raw)) if raw else MonitoredASIN(asin=asin, platform="amazon", label="manual")
        snapshot = await save_snapshot(redis, f"{account}:snapshots:{asin}", monitored)
    return asdict(snapshot)
