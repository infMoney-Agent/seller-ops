import asyncio
from app.workers.celery_app import celery_app
from app.core.logging import logger


@celery_app.task(bind=True, name="app.workers.tasks.monitor_tasks.run_scheduled_monitors", max_retries=3)
def run_scheduled_monitors(self):
    """Periodic task: take snapshots for all monitored ASINs stored in Redis."""
    asyncio.run(_run_monitors())


async def _run_monitors():
    from datetime import datetime, timedelta, timezone
    import json

    from redis.asyncio import Redis

    from app.core.config import get_settings
    from app.modules.competitor_monitor import MonitoredASIN
    from app.modules.competitor_monitor.storage import save_snapshot

    async with Redis.from_url(get_settings().REDIS_URL) as redis:
        async for key in redis.scan_iter(match="seller:*:monitor:*"):
            raw = await redis.get(key)
            if not raw:
                continue
            monitored = MonitoredASIN(**json.loads(raw))
            account = key.decode().rsplit(":", 2)[0]
            snapshot_key = f"{account}:snapshots:{monitored.asin}"
            try:
                previous = await redis.lindex(snapshot_key, 0)
                if previous:
                    timestamp = datetime.fromisoformat(json.loads(previous)["timestamp"])
                    interval = timedelta(minutes=monitored.check_interval_minutes)
                    if datetime.now(timezone.utc) - timestamp < interval:
                        continue
                snapshot = await save_snapshot(redis, snapshot_key, monitored)
                logger.info("snapshot_taken", asin=monitored.asin, price=snapshot.price)
            except Exception as exc:
                logger.error("snapshot_failed", asin=monitored.asin, error=str(exc))


@celery_app.task(name="app.workers.tasks.monitor_tasks.scrape_product_research")
def scrape_product_research(category: str, limit: int = 50):
    """On-demand task: run product research for a category."""
    return asyncio.run(_research(category, limit))


async def _research(category: str, limit: int):
    import dataclasses
    from app.modules.product_research import research_category
    scores = await research_category(category, limit=limit)
    return [dataclasses.asdict(s) for s in scores]
