import os
from contextlib import asynccontextmanager
from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

_engine = None


def engine():
    global _engine
    if _engine is None:
        url = os.environ.get("SCREENING_DATABASE_URL")
        if not url:
            raise RuntimeError("SCREENING_DATABASE_URL is not configured")
        _engine = create_async_engine(
            url, pool_pre_ping=True, pool_size=5, max_overflow=5
        )
    return _engine


@asynccontextmanager
async def transaction(tenant=None):
    async with engine().begin() as conn:
        if tenant is not None:
            await conn.execute(
                text("SELECT pg_advisory_xact_lock(:tenant)"), {"tenant": tenant}
            )
        yield conn


async def rows(conn, sql, **params):
    return [dict(r) for r in (await conn.execute(text(sql), params)).mappings().all()]


async def one(conn, sql, **params):
    result = await rows(conn, sql, **params)
    return result[0] if result else None


async def execute(conn, sql, **params):
    return await conn.execute(text(sql), params)
