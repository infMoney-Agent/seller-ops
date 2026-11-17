import json
from dataclasses import asdict

from redis.asyncio import Redis

from app.modules.competitor_monitor.models import CompetitorSnapshot, MonitoredASIN
from app.modules.competitor_monitor.monitor import take_snapshot


async def save_snapshot(
    redis: Redis, key: str, monitored: MonitoredASIN
) -> CompetitorSnapshot:
    previous = await redis.lindex(key, 0)
    if previous:
        monitored.snapshots = [CompetitorSnapshot(**json.loads(previous))]
    snapshot = await take_snapshot(monitored)
    async with redis.pipeline(transaction=True) as pipeline:
        pipeline.lpush(key, json.dumps(asdict(snapshot)))
        pipeline.ltrim(key, 0, 999)
        await pipeline.execute()
    return snapshot
