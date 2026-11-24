package ecom

import (
	"context"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserProfitTable(t *testing.T) {
	base := os.Getenv("SCREENING_WEB_QA_URL")
	if base == "" {
		t.Skip("isolated browser fixture required")
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.Flag("no-sandbox", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.WindowSize(1920, 1080))
	if binary := os.Getenv("CHROME_BIN"); binary != "" {
		options = append(options, chromedp.ExecPath(binary))
	}
	alloc, closeAlloc := chromedp.NewExecAllocator(context.Background(), options...)
	defer closeAlloc()
	ctx, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	ctx, timeout := context.WithTimeout(ctx, 75*time.Second)
	defer timeout()
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/dashboard/ecom/screening"), chromedp.WaitReady("#main-content", chromedp.ByQuery), chromedp.Evaluate(`document.querySelectorAll('button').forEach(b=>{if(b.textContent.trim()==='利润测算')b.click()})`, nil), chromedp.Poll(`document.querySelectorAll('[data-profit-row]').length===2`, nil)); err != nil {
		t.Fatal("profit table load", err)
	}
	var details string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{const panel=document.querySelector('[data-profit-table-panel]');const headers=[...panel.querySelectorAll('th')].map(x=>x.textContent.trim());if(!headers.some(h=>h.includes('供货价'))||!headers.some(h=>h.includes('单件利润 CNY')))throw Error('profit columns missing');if(document.documentElement.scrollWidth>innerWidth+1)throw Error('table overflow escaped its scroll region');const row=[...panel.querySelectorAll('[data-profit-row]')].find(r=>r.textContent.includes('PROFIT0001'));if(!row.textContent.includes('422.70 CNY'))throw Error('precise initial CNY result missing');const input=row.querySelector('input[aria-label="PROFIT0001 供货价 CNY"]');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'71');input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));return 'ok';})()`, &details), chromedp.Poll(`[...document.querySelectorAll('[data-profit-row]')].find(r=>r.textContent.includes('PROFIT0001'))?.textContent.includes('421.70 CNY')`, nil), chromedp.Poll(`[...document.querySelectorAll('[data-profit-row]')].find(r=>r.textContent.includes('PROFIT0001'))?.textContent.includes('已保存')`, nil)); err != nil {
		t.Fatal("inline calculate/save", details, err)
	}
	verify := `(async()=>{const data=await(await fetch('/api/ecom/screening/profit/products?product_id=11111111-1111-4111-8111-111111111111')).json();const p=data.items[0];if(p.scenario.inputs.purchase_cny!=='71'||p.scenario.output.profit_cny!=='421.70')throw Error('saved snapshot incorrect');const history=await(await fetch('/api/ecom/screening/products/'+p.id+'/profit-scenarios')).json();if(history.total!==2)throw Error('previous scenario overwritten');return JSON.stringify({rows:document.querySelectorAll('[data-profit-row]').length,profit:p.scenario.output.profit_cny,history:history.total,pageOverflow:document.documentElement.scrollWidth>innerWidth+1})})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(verify, &details, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatal(err)
	}
	t.Log(details)
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		var image []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&image, 90)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profit-table-wide.png"), image, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('[data-profit-row] button').forEach(b=>{if(b.textContent.trim()==='参数 / 历史'&&!window.__openedProfit){window.__openedProfit=true;b.click()}})`, nil), chromedp.Poll(`!!document.querySelector('form[aria-label="利润测算方案"]')`, nil)); err != nil {
		t.Fatal("scenario editor not retained", err)
	}
}
