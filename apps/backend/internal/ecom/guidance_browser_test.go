package ecom

import (
	"context"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func checkBrowserGuidance(t *testing.T, ctx context.Context, base string) {
	t.Helper()
	step := 0
	run := func(actions ...chromedp.Action) {
		step++
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			var body string
			chromedp.Run(ctx, chromedp.Evaluate("JSON.stringify([...document.querySelectorAll(\"form input[name]\")].map(x=>[x.name,x.value]))+document.body.innerText", &body))
			t.Fatalf("Guidance step %d: %v; page: %s", step, err, body)
		}
	}
	fill := func(name, value string) {
		t.Helper()
		selector := "form[aria-label='利润测算方案'] input[name='" + name + "']"
		run(chromedp.Focus(selector, chromedp.ByQuery), chromedp.KeyEvent("a", chromedp.KeyModifiers(input.ModifierCtrl)), chromedp.KeyEvent(kb.Backspace), chromedp.SendKeys(selector, value, chromedp.ByQuery))
	}
	run(
		chromedp.Evaluate("(()=>{const popup=document.querySelector('[data-slot=popover-content]');if(popup)[...popup.querySelectorAll('button')].find(b=>b.textContent.trim()==='关闭').click();const area=document.querySelector('[data-screening-table]');if(area.tabIndex!==0||area.getAttribute('role')!=='region'||!area.getAttribute('aria-label')||!area.querySelector('caption'))throw Error('Named keyboard table region missing');const row=[...document.querySelectorAll('tbody tr')].find(r=>r.textContent.includes('BROWSER001'));[...row.querySelectorAll('button')].find(b=>b.textContent.trim()==='测算').click()})()", nil),
		chromedp.WaitVisible("form[aria-label='利润测算方案']", chromedp.ByQuery),
	)
	run(chromedp.Poll("(async()=>{const r=await fetch('/api/ecom/screening/products?show_merged=false&page=1');if(r.headers.get('content-encoding')!=='gzip'||!r.headers.get('server-timing')?.includes('auth;dur='))throw Error('Compressed list or server timing missing');const data=await r.json();return data.items.length>0})()", nil))
	fill("sale_price_usd", "19.99")
	fill("fx_cny_per_usd", "0")
	run(chromedp.Evaluate("window.__profitRequests=performance.getEntriesByType('resource').filter(x=>x.name.includes('/profit/calculate')).length;document.querySelector('button[value=compare]').click()", nil),
		chromedp.Poll("document.activeElement?.name==='fx_cny_per_usd' && document.activeElement.getAttribute('aria-invalid')==='true'", nil),
		chromedp.Evaluate("if(performance.getEntriesByType('resource').filter(x=>x.name.includes('/profit/calculate')).length!==window.__profitRequests)throw Error('Invalid FX reached API')", nil))
	for name, value := range map[string]string{"fx_cny_per_usd": "7.20", "discount_usd": "0", "purchase_cny": "18", "packaging_cny": "2", "freight_cny": "8", "other_landed_cny": "0", "commission_rate": "0.15", "fba_fee_usd": "4", "storage_usd": "0.10", "ad_rate": "0.10", "return_rate": "0.03", "loss_per_return_usd": "12", "other_usd": "0", "monthly_units": "100"} {
		fill(name, value)
	}
	run(chromedp.Evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='复制为新方案').click()", nil), chromedp.Poll("document.querySelector('form input[name=name]').value==='方案2'", nil))
	fill("commission_rate", "1.5")
	run(chromedp.Evaluate("[...document.querySelectorAll('form button')].find(b=>b.textContent.trim()==='基准').click()", nil),
		chromedp.Poll("document.querySelector('form input[name=name]').value==='基准'", nil),
		chromedp.Evaluate("document.querySelector('button[value=compare]').click()", nil),
		chromedp.Poll("document.querySelector('form input[name=name]').value==='方案2' && document.activeElement?.name==='commission_rate' && document.activeElement.value==='1.5' && document.activeElement.getAttribute('aria-invalid')==='true'", nil))
	fill("commission_rate", "0.15")
	run(chromedp.Click("button[value=compare]", chromedp.ByQuery),
		chromedp.Poll("[...document.querySelectorAll('form tbody tr')].filter(row=>row.textContent.includes('6.64')).length===2 && !document.querySelector('form fieldset').disabled", nil),
		chromedp.Evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='导入记录').click()", nil),
		chromedp.Poll("[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='查看')", nil),
		chromedp.Evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='查看').click()", nil),
		chromedp.Poll("document.body.innerText.includes('导入完成，可前往商品库查看')", nil),
		chromedp.Sleep(3*time.Second),
		chromedp.Evaluate("window.__importReads=performance.getEntriesByType('resource').filter(x=>new URL(x.name).pathname.includes('/screening/imports')).length", nil),
		chromedp.Sleep(4500*time.Millisecond),
		chromedp.Evaluate("if(performance.getEntriesByType('resource').filter(x=>new URL(x.name).pathname.includes('/screening/imports')).length!==window.__importReads)throw Error('Completed imports still polling')", nil),
	)
	// These mocks are installed only in the isolated browser fixture, never application code.
	script := `(()=>{
 const original=window.fetch.bind(window);
 window.__clipboardMode='denied';
 Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async()=>{if(window.__clipboardMode==='denied')throw new DOMException('Denied','NotAllowedError')}}});
 window.fetch=async(input,init)=>{
  const url=new URL(typeof input==='string'?input:input.url,location.href);
  if(url.pathname==='/api/ecom/screening/products' && url.searchParams.get('q')==='slow-list')await new Promise(resolve=>setTimeout(resolve,650));
  if(url.pathname==='/api/ecom/screening/products' && url.searchParams.get('q')==='prefetch'){
    const page=Number(url.searchParams.get('page')||1);window.__pageReads=window.__pageReads||[];window.__pageReads.push(page);
    const response=await original('/api/ecom/screening/products?show_merged=false&page=1');const data=await response.json();data.total=100;data.page=page;data.items=data.items.map(item=>({...item,title:'prefetch-page-'+page,title_zh:'prefetch-page-'+page}));return Response.json(data);
  }
  if(url.pathname==='/api/ecom/monitor/list')return Response.json({monitors:[{asin:'MONITOR001',label:'监控测试'}]});
  if(url.pathname.startsWith('/api/ecom/monitor/snapshots/'))return Response.json({snapshots:[3,2,1].map(day=>({timestamp:'2026-08-0'+day+'T00:00:00Z',price:10+day,bsr_rank:100*day,alert_triggered:false}))});
  if(url.pathname==='/api/ecom/listing/generate')return Response.json({title:'测试商品标题',bullet_points:['one','two','three','four','five'],description:'测试商品描述',search_terms:['tool'],a_plus_draft:'草稿',seo_score:8,character_counts:{title:6,description:6,search_terms:4}});
  return original(input,init);
 };
})()`
	run(chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
		return err
	}),
		chromedp.Navigate(base+"/dashboard/ecom/monitor"),
		chromedp.WaitVisible("button[aria-label='查看监控 MONITOR001']", chromedp.ByQuery),
		chromedp.Focus("button[aria-label='查看监控 MONITOR001']", chromedp.ByQuery), chromedp.KeyEvent(kb.Enter),
		chromedp.Poll("document.querySelector('button[aria-label=\"查看监控 MONITOR001\"]')?.getAttribute('aria-pressed')==='true' && document.querySelectorAll('.recharts-xAxis-tick-labels text').length>0", nil),
		chromedp.Evaluate("window.__axis=[...document.querySelectorAll('.recharts-xAxis-tick-labels text')].map(x=>x.textContent).join('|');if(!document.querySelector('button[aria-label=\"删除监控 MONITOR001\"]'))throw Error('Delete name missing');for(const input of document.querySelectorAll('input'))if(!input.labels?.length)throw Error('Unlabelled monitor input')", nil),
		chromedp.SendKeys("input[aria-labelledby$='-note-label']", "x", chromedp.ByQuery),
		chromedp.Evaluate("if([...document.querySelectorAll('.recharts-xAxis-tick-labels text')].map(x=>x.textContent).join('|')!==window.__axis)throw Error('Cached snapshot order changed on rerender')", nil),
		chromedp.Navigate(base+"/dashboard/ecom/listing"),
		chromedp.WaitVisible("input[placeholder^='例如：32']", chromedp.ByQuery),
		chromedp.Evaluate("for(const input of document.querySelectorAll('input,textarea,select'))if(!input.labels?.length)throw Error('Unlabelled listing field')", nil),
		chromedp.SendKeys("input[placeholder^='例如：32']", "fixture tool", chromedp.ByQuery),
		chromedp.Evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='生成商品页面文案').click()", nil),
		chromedp.WaitVisible("button[aria-label='复制商品标题']", chromedp.ByQuery),
		chromedp.Click("button[aria-label='复制商品标题']", chromedp.ByQuery),
		chromedp.Poll("[...document.querySelectorAll('[role=status]')].some(x=>x.textContent.includes('复制失败'))", nil),
		chromedp.Evaluate("window.__clipboardMode='ok'", nil),
		chromedp.Click("button[aria-label='复制商品标题']", chromedp.ByQuery),
		chromedp.Poll("[...document.querySelectorAll('[role=status]')].some(x=>x.textContent.includes('已复制'))", nil),
		chromedp.Navigate(base+"/dashboard/chat"),
		chromedp.WaitVisible("#messenger-editor", chromedp.ByQuery),
		chromedp.SendKeys("#messenger-editor", "组合输入测试", chromedp.ByQuery),
		chromedp.Evaluate("(()=>{window.__submits=0;const input=document.querySelector('#messenger-editor');input.form.addEventListener('submit',()=>window.__submits++,true);for(const options of [{isComposing:true,keyCode:229},{isComposing:false,keyCode:229},{isComposing:false,keyCode:13,shiftKey:true}])input.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,...options}));if(window.__submits!==0)throw Error('IME or Shift+Enter submitted');})()", nil),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Poll("window.__submits===1", nil),
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(base+"/dashboard/ecom/screening"),
		chromedp.WaitVisible("[data-screening-table]", chromedp.ByQuery),
		chromedp.Focus("[data-screening-table]", chromedp.ByQuery), chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Poll("document.querySelector('[data-screening-table]').scrollLeft>0", nil),
	)

	run(chromedp.Focus("input[placeholder='搜索 ASIN 或商品标题']", chromedp.ByQuery), chromedp.ActionFunc(func(ctx context.Context) error { return input.InsertText("slow-list").Do(ctx) }),
		chromedp.Poll("!!document.querySelector('[data-screening-table][aria-busy=true]') && document.querySelector('[data-screening-table] tbody').inert && document.querySelector('[data-screening-table]').textContent.includes('BROWSER001')", nil),
		chromedp.Poll("document.body.innerText.includes('没有符合筛选条件的商品') && !document.querySelector('[data-screening-table]').closest('fieldset').disabled", nil),
		chromedp.Focus("input[placeholder='搜索 ASIN 或商品标题']", chromedp.ByQuery), chromedp.KeyEvent("a", chromedp.KeyModifiers(input.ModifierCtrl)), chromedp.KeyEvent(kb.Backspace),
		chromedp.ActionFunc(func(ctx context.Context) error { return input.InsertText("prefetch").Do(ctx) }),
		chromedp.Poll("window.__pageReads?.includes(2) && document.body.innerText.includes('prefetch-page-1')", nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='下一页').click()", nil),
		chromedp.Poll("document.body.innerText.includes('prefetch-page-2') && window.__pageReads.filter(page=>page===2).length===1", nil))
	var shot []byte
	run(chromedp.FullScreenshot(&shot, 80))
	dir := env("SCREENING_QA_DIR", os.TempDir()+"/screening-go-qa")
	if err := os.WriteFile(filepath.Join(dir, "guidance-mobile.png"), shot, 0640); err != nil {
		t.Fatal(err)
	}
	t.Log("Guidance browser checks passed: profit validation/focus, completed polling, monitor keyboard/order, labels, clipboard denial/success, IME events, keyboard table scroll")
}
