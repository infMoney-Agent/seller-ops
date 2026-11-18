"""Run after screening_browser.py against the same isolated fixture workspace."""

import asyncio
import os
from pathlib import Path
import re
from playwright.async_api import async_playwright, expect


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch(
            headless=True,
            args=["--no-sandbox"],
            executable_path=os.environ.get("SCREENING_BROWSER_EXECUTABLE"),
        )
        page = await browser.new_page(viewport={"width": 1440, "height": 1000})
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        await page.goto("http://127.0.0.1:19443/dashboard/ecom/screening")
        await page.get_by_role("button", name="导入记录", exact=True).click()
        await page.get_by_label("选择商品文件").set_input_files(
            {
                "name": "browser-october.csv",
                "mimeType": "text/csv",
                "buffer": b"ASIN,Title,Price,Monthly Sales\nBROWSER001,Manual steel tray,18,900\n",
            }
        )
        await page.get_by_label("来源期", exact=True).fill("2026-10")
        await page.get_by_label("观察日期（确认导入前必填）").fill("2026-10-31")
        await page.get_by_role("button", name="上传并预览").click()
        await expect(
            page.get_by_role("button", name="确认导入", exact=True)
        ).to_be_enabled(timeout=20000)
        await page.get_by_role("button", name="确认导入", exact=True).click()
        await page.get_by_role("button", name="商品库", exact=True).click()
        months = page.get_by_label("经营数据月份", exact=True)
        await expect(
            months.locator('option[value="2026-10|calendar_month"]')
        ).to_be_attached(timeout=20000)
        await expect(
            page.get_by_role("cell", name=re.compile(r"^\$20"))
        ).to_be_visible()
        await months.select_option("2026-10|calendar_month")
        await expect(
            page.get_by_role("cell", name=re.compile(r"^\$18"))
        ).to_be_visible()
        await page.get_by_role("button", name="BROWSER001", exact=True).click()
        history = page.get_by_role("heading", name="月度经营数据", exact=True).locator(
            ".."
        )
        await expect(
            history.get_by_role("row").filter(has_text="2026-11")
        ).to_be_visible()
        await expect(
            history.get_by_role("row").filter(has_text="2026-10")
        ).to_be_visible()
        await (
            history.get_by_role("row")
            .filter(has_text="2026-10")
            .get_by_role("button", name="用此期测算")
            .click()
        )
        await expect(
            page.get_by_text(re.compile("测算数据来源：2026-10"))
        ).to_be_visible()
        await page.get_by_role("button", name="使用所选期售价").click()
        await expect(page.get_by_label("售价 USD", exact=True)).to_have_value(
            re.compile(r"^18(?:\.0+)?$")
        )
        await page.get_by_role("button", name="商品库", exact=True).click()
        await months.select_option("")
        await expect(
            page.get_by_role("cell", name=re.compile(r"^\$20"))
        ).to_be_visible()
        await page.get_by_role("button", name="BROWSER001", exact=True).click()
        await expect(
            page.get_by_role("heading", name="月度经营数据", exact=True)
        ).to_be_visible()
        await page.evaluate("window.scrollTo(0,0)")
        output = Path(os.environ.get("SCREENING_QA_DIR", "/tmp/screening-qa"))
        output.mkdir(parents=True, exist_ok=True)
        await page.screenshot(
            path=str(output / "screening-monthly-desktop.png"), full_page=True
        )
        await page.set_viewport_size({"width": 390, "height": 844})
        dimensions = await page.evaluate(
            "({width:innerWidth,scroll:document.documentElement.scrollWidth})"
        )
        assert dimensions["scroll"] <= dimensions["width"] + 1, dimensions
        await page.screenshot(
            path=str(output / "screening-monthly-mobile.png"), full_page=True
        )
        assert not errors, errors
        await browser.close()
        print(
            "Monthly browser acceptance passed: two months preserved, month picker switches metrics, historical price used in profit form, latest month stays unchanged"
        )


asyncio.run(main())
