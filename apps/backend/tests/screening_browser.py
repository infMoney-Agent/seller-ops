"""Browser acceptance against loopback-only fake identity + real screening PostgreSQL."""

import asyncio
import os
from pathlib import Path
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
        await expect(
            page.get_by_role("heading", name="产品初筛", exact=True)
        ).to_be_visible()
        await page.get_by_role("button", name="导入记录", exact=True).click()
        await page.get_by_label("选择商品文件").set_input_files(
            {
                "name": "browser-test.csv",
                "mimeType": "text/csv",
                "buffer": b"ASIN,Title,Price\nBROWSER001,Manual steel tray,20\n",
            }
        )
        await page.get_by_label("来源期", exact=True).fill("2026-11")
        await page.get_by_label("观察日期（确认导入前必填）").fill("2026-11-18")
        await page.get_by_role("button", name="上传并预览").click()
        await expect(
            page.get_by_role("button", name="确认导入", exact=True)
        ).to_be_enabled(timeout=20000)
        await page.get_by_role("button", name="确认导入", exact=True).click()
        await page.get_by_role("button", name="商品库", exact=True).click()
        await expect(
            page.get_by_role("button", name="BROWSER001", exact=True)
        ).to_be_visible(timeout=20000)
        await page.get_by_label("选择 BROWSER001").check()
        await page.get_by_role("button", name="批量复核", exact=True).first.click()
        await page.get_by_role("button", name="生成批量预览").click()
        await expect(
            page.get_by_role("button", name="确认保存人工结论")
        ).to_be_visible()
        await page.get_by_role("button", name="确认保存人工结论").click()
        await expect(page.get_by_text("批量复核已执行", exact=True)).to_be_visible()
        await page.get_by_role("button", name="撤销此批次").first.click()
        await expect(
            page.get_by_text("撤销已完成，后续修改不会覆盖", exact=True)
        ).to_be_visible()
        await page.get_by_role("button", name="商品库", exact=True).click()
        await page.get_by_role("button", name="测算", exact=True).first.click()
        fields = {
            "售价 USD": "20",
            "单件优惠 USD": "0",
            "采购价 CNY": "14",
            "包装 CNY": "0",
            "头程 CNY": "7",
            "其他落地成本 CNY": "0",
            "汇率（1 USD = CNY）": "7",
            "佣金率（0至1）": "0.15",
            "FBA 履约费 USD": "4",
            "单件仓储费 USD": "0",
            "广告费率（0至1）": "0.1",
            "退货率（0至1）": "0.1",
            "每次退货净损失 USD": "2",
            "其他费用 USD": "0",
            "预计月销量": "100",
        }
        for label, value in fields.items():
            await page.get_by_label(label, exact=True).fill(value)
        await page.get_by_role("button", name="比较全部方案").click()
        await expect(page.get_by_role("cell", name="7.80", exact=True)).to_be_visible()
        await page.get_by_role("button", name="保存当前方案").click()
        await expect(page.get_by_text("方案快照已保存", exact=True)).to_be_visible()
        await page.get_by_role("button", name="商品库", exact=True).click()
        await page.get_by_role("button", name="导出已选商品").click()
        download_button = page.get_by_role("button", name="下载 Excel")
        await expect(download_button).to_be_visible(timeout=20000)
        async with page.expect_download() as event:
            await download_button.click()
        result = await event.value
        assert result.suggested_filename.endswith(".xlsx")
        output = Path(os.environ.get("SCREENING_QA_DIR", "/tmp/screening-qa"))
        output.mkdir(parents=True, exist_ok=True)
        await page.evaluate("window.scrollTo(0,0)")
        await page.screenshot(
            path=str(output / "screening-desktop.png"), full_page=True
        )
        await page.set_viewport_size({"width": 390, "height": 844})
        await page.evaluate("window.scrollTo(0,0)")
        dimensions = await page.evaluate(
            "({width:innerWidth,scroll:document.documentElement.scrollWidth})"
        )
        assert dimensions["scroll"] <= dimensions["width"] + 1, dimensions
        await page.screenshot(path=str(output / "screening-mobile.png"), full_page=True)
        assert not errors, errors
        await browser.close()
        print(
            "Browser acceptance passed: upload → commit → batch apply/undo → profit 7.80 → save → export; desktop/mobile screenshots saved"
        )


asyncio.run(main())
