package ecom

import (
	"context"
	"fmt"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowserScreening(t *testing.T) {
	base := os.Getenv("SCREENING_WEB_QA_URL")
	if base == "" {
		t.Skip("browser acceptance disabled")
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.Flag("no-sandbox", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.WindowSize(1440, 1100))
	if binary := os.Getenv("CHROME_BIN"); binary != "" {
		options = append(options, chromedp.ExecPath(binary))
	}
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()
	ctx, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	ctx, timeout := context.WithTimeout(ctx, 2*time.Minute)
	defer timeout()
	var text string
	if e := chromedp.Run(ctx, chromedp.Navigate(base+"/dashboard/ecom/screening"), chromedp.WaitReady(`#main-content`, chromedp.ByQuery), chromedp.Text("body", &text)); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "产品初筛") {
		t.Fatal("screening page did not render", text)
	}
	if strings.Contains(text, "Vercel") {
		t.Fatal("template theme selector still visible")
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='更多筛选');if(!b||b.getAttribute('aria-expanded')!=='false'||document.getElementById('screening-advanced-filters'))throw Error('Advanced filters must start collapsed');b.click()})()`, nil),
		chromedp.Poll(`!!document.getElementById('screening-advanced-filters')`, nil),
		chromedp.Evaluate(`(()=>{const label=[...document.querySelectorAll('#screening-advanced-filters label')].find(l=>l.textContent.includes('推荐类型'));const select=label.querySelector('select');select.value='exploration';select.dispatchEvent(new Event('change',{bubbles:true}));})()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.includes('重置筛选（1）'))`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.includes('重置筛选')).click()`, nil),
		chromedp.Poll(`!document.getElementById('screening-advanced-filters') && [...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='重置筛选'&&b.disabled)`, nil),
	); e != nil {
		t.Fatalf("compact filters failed: %v", e)
	}
	// Exercise actual import controls as well as the HTTP workflow below.
	fixturePath := filepath.Join(t.TempDir(), "Product-Tools-US-2026.08-ui.csv")
	if e := os.WriteFile(fixturePath, []byte("ASIN,商品标题,月销量,价格($),销量口径,父ASIN,商品尺寸（单位换算）,商品重量（单位换算）,包装尺寸（单位换算）,包装重量（单位换算）,BuyBox卖家,卖家所属地,FBA($),毛利率\nGUI0000001,manual storage tool,100,19.99,child,PARENT0001,22.86 x 7.62 x 17.78 cm,154.22 g,25.65 x 21.59 x 8.13 cm,181.44 g,Fixture Seller,US,5.91,70%\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='导入商品').click()`, nil),
		chromedp.WaitVisible(`[aria-label="选择商品文件"]`, chromedp.ByQuery),
		chromedp.SetUploadFiles(`[aria-label="选择商品文件"]`, []string{fixturePath}, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('dialog[open] button').click()`, nil),
		chromedp.Poll(`!document.querySelector('dialog[open]')`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='导入商品').click()`, nil),
		chromedp.Poll(`!!document.querySelector('dialog[open]') && document.querySelector('[aria-label="选择商品文件"]').files.length===1`, nil),

		chromedp.Poll(`[...document.querySelectorAll('input')].some(x=>x.value==='2026-08') && !!document.querySelector('input[type=date]')?.value`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='上传并预览').click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='确认导入'&&!b.disabled)`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var shot []byte
			if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 80)); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(os.Getenv("SCREENING_QA_DIR"), "import-preview.png"), shot, 0640)
		}),
		chromedp.Evaluate(`(()=>{if(document.querySelector('select[aria-label^="映射 "]')||document.body.innerText.includes('调整日期与字段对应关系')||[...document.querySelectorAll('button')].some(b=>b.textContent.includes('重新解析')))throw Error('manual mapping UI remains');if(!document.querySelector('dialog[open]'))throw Error('import dialog missing');if(document.body.innerText.includes('后台任务'))throw Error('technical import task UI remains');})()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认导入').click()`, nil),
		chromedp.Poll(`(async()=>{const r=await fetch('/api/ecom/screening/products?q=GUI0000001&show_merged=true');const v=await r.json();return r.ok&&v.total===1})()`, nil),
	); e != nil {
		var body string
		chromedp.Run(ctx, chromedp.Text(`body`, &body, chromedp.ByQuery))
		t.Fatalf("import controls failed: %v; page: %s", e, body)
	}

	multiA := filepath.Join(t.TempDir(), "Product-Multi-US-2026.09-a.csv")
	multiB := filepath.Join(t.TempDir(), "Product-Multi-US-2026.10-b.csv")
	for path, content := range map[string]string{multiA: "ASIN,商品标题\nMULTI00001,multi fixture one\n", multiB: "ASIN,商品标题\nMULTI00002,multi fixture two\n"} {
		if e := os.WriteFile(path, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[aria-label="选择商品文件"]').closest('details').open=true`, nil),
		chromedp.SetUploadFiles(`[aria-label="选择商品文件"]`, []string{multiA, multiB}, chromedp.ByQuery),
		chromedp.Evaluate("window.__multiStep=1", nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='上传并预览'&&!b.disabled)`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='上传并预览').click()`, nil),
		chromedp.Evaluate("window.__multiStep=2", nil),
		chromedp.Poll(`(async()=>{const v=await(await fetch('/api/ecom/screening/imports')).json();return v.items.filter(i=>i.file_name.includes('Product-Multi-')&&i.state==='preview').length===2})()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认本次全部文件').click()`, nil),
		chromedp.Evaluate("window.__multiStep=3", nil),
		chromedp.Poll(`(async()=>{const v=await(await fetch('/api/ecom/screening/products?q=MULTI&show_merged=true')).json();return v.total===2})()`, nil),
		chromedp.Evaluate(`document.querySelector('[aria-label="导入记录，可横向滚动"]').closest('details').open=true`, nil),
		chromedp.Evaluate("window.__multiStep=4", nil),
		chromedp.Poll(`[...document.querySelectorAll('[aria-label="导入记录，可横向滚动"] tbody tr')].some(r=>r.textContent.includes('Product-Multi-US-2026.10-b.csv')&&[...r.querySelectorAll('button')].some(b=>b.textContent.trim()==='删除导入'&&!b.disabled))`, nil),
		chromedp.Evaluate(`(()=>{const r=[...document.querySelectorAll('[aria-label="导入记录，可横向滚动"] tbody tr')].find(r=>r.textContent.includes('Product-Multi-US-2026.10-b.csv'));[...r.querySelectorAll('button')].find(b=>b.textContent.trim()==='删除导入').click()})()`, nil),
		chromedp.Evaluate("window.__multiStep=5", nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='确认删除数据'&&!b.disabled)`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认删除数据').click()`, nil),
		chromedp.Evaluate("window.__multiStep=6", nil),
		chromedp.Poll(`(async()=>{const v=await(await fetch('/api/ecom/screening/imports')).json();const p=await(await fetch('/api/ecom/screening/products?q=MULTI&show_merged=true')).json();return !v.items.some(i=>i.file_name==='Product-Multi-US-2026.10-b.csv')&&p.total===1})()`, nil),
		chromedp.Evaluate(`document.querySelector('[aria-label="选择商品文件"]').closest('details').open=true`, nil),
		chromedp.SetUploadFiles(`[aria-label="选择商品文件"]`, []string{multiB}, chromedp.ByQuery),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='上传并预览'&&!b.disabled)`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='上传并预览').click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='确认导入'&&!b.disabled)`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认导入').click()`, nil),
		chromedp.Poll(`(async()=>{const v=await(await fetch('/api/ecom/screening/products?q=MULTI&show_merged=true')).json();return v.total===2})()`, nil),
	); e != nil {
		var body string
		chromedp.Run(ctx, chromedp.Evaluate("String(window.__multiStep)+document.body.innerText", &body))
		t.Fatal("multi upload or history removal failed", e, body)
	}
	// Exercise the same-origin proxy in the browser, including Origin, real session
	// lookup and HMAC forwarding, using a file whose only contents are fixtures.
	script := `(async()=>{const h={'Idempotency-Key':crypto.randomUUID()};const f=new FormData();f.set('file',new Blob(['ASIN,商品标题,月销量,价格($),销量口径,父ASIN\nBROWSER001,manual kitchen tool,120,19.99,child,PARENT0001\n'],{type:'text/csv'}),'Product-Tools-US-2026.08-qa.csv');f.set('source_type','manual');f.set('period_type','calendar_month');f.set('default_sales_scope','child');const response=await fetch('/api/ecom/screening/imports',{method:'POST',headers:h,body:f});if(!response.ok)throw Error(await response.text());const job=await response.json();const batch=await(await fetch('/api/ecom/screening/imports/'+job.import_id,{headers:h})).json();if(batch.source_period!=='2026-08'||!batch.observed_on)throw Error('Import date defaults failed: '+JSON.stringify(batch));let parsed;for(let i=0;i<50;i++){await new Promise(r=>setTimeout(r,250));const j=await(await fetch('/api/ecom/screening/jobs/'+job.job_id,{headers:h})).json();if(j.state==='failed')throw Error(JSON.stringify(j));if(j.state==='succeeded'){parsed=j;break}}if(!parsed)throw Error('parse timed out');const commit=await fetch('/api/ecom/screening/imports/'+job.import_id+'/commit',{method:'POST',headers:{...h,'Idempotency-Key':crypto.randomUUID()}});if(!commit.ok)throw Error(await commit.text());const c=await commit.json();let finished;for(let i=0;i<50;i++){await new Promise(r=>setTimeout(r,250));const j=await(await fetch('/api/ecom/screening/jobs/'+c.job_id,{headers:h})).json();if(j.state==='failed')throw Error(JSON.stringify(j));if(j.state==='succeeded'||j.state==='partial'){finished=j;break}}if(!finished)throw Error('import timed out');const screenId=finished.result?.screen?.job_id;if(screenId){let screened=false;for(let i=0;i<50;i++){await new Promise(r=>setTimeout(r,250));const sj=await(await fetch('/api/ecom/screening/jobs/'+screenId,{headers:h})).json();if(sj.state==='failed')throw Error(JSON.stringify(sj));if(sj.state==='succeeded'||sj.state==='partial'){screened=true;break}}if(!screened)throw Error('screen timed out')}const p=await(await fetch('/api/ecom/screening/products?q=BROWSER001&show_merged=true',{headers:h})).json();if(p.total!==1)throw Error(JSON.stringify(p));const v=p.items[0];const review=await fetch('/api/ecom/screening/products/'+v.id+'/review',{method:'PATCH',headers:{...h,'Idempotency-Key':crypto.randomUUID(),'Content-Type':'application/json'},body:JSON.stringify({expected_data_revision:v.data_revision,expected_review_revision:v.review_revision,status:'kept',reason:'浏览器验证'})});if(!review.ok)throw Error(await review.text());const profit=await fetch('/api/ecom/screening/profit/calculate',{method:'POST',headers:{...h,'Content-Type':'application/json'},body:JSON.stringify({product_id:v.id,expected_data_revision:v.data_revision,scenarios:[{name:'浏览器方案',sale_price_usd:'19.99',discount_usd:'0',purchase_cny:'18',packaging_cny:'2',freight_cny:'8',other_landed_cny:'0',fx_cny_per_usd:'7.20',commission_rate:'0.15',fba_fee_usd:'4.00',storage_usd:'0.10',ad_rate:'0.10',return_rate:'0.03',loss_per_return_usd:'12',other_usd:'0',monthly_units:100}]})});const result=await profit.json();if(!profit.ok||result.items?.[0]?.output?.profit_usd!=='6.64')throw Error(JSON.stringify(result));const listing=await fetch('/api/ecom/listing/generate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({keyword:'tool',language:'de'})});const listingData=await listing.json();if(!listing.ok||listingData.language!=='de'||listingData.bullet_points.length!==5)throw Error(JSON.stringify(listingData));const categories=await fetch('/api/ecom/product-research/categories');const categoryData=await categories.json();if(!categories.ok||!categoryData.categories.includes('home'))throw Error(JSON.stringify(categoryData));return 'ok'})()`
	var result string
	if e := chromedp.Run(ctx, chromedp.Evaluate(script, &result, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); e != nil {
		t.Fatal(e)
	}
	if result != "ok" {
		t.Fatal(result)
	}
	if e := chromedp.Run(ctx, chromedp.Reload(), chromedp.WaitReady(`#main-content`, chromedp.ByQuery), chromedp.Poll(`document.body.innerText.includes('BROWSER001')`, nil)); e != nil {
		t.Fatal(e)
	}

	for _, width := range []int64{1366, 1440, 1920} {
		if e := chromedp.Run(ctx, chromedp.EmulateViewport(width, 1000), chromedp.Poll(`(()=>{const area=document.querySelector('[data-screening-table]');return area&&document.documentElement.scrollWidth<=innerWidth+2&&[...area.querySelectorAll('tbody td')].every(c=>getComputedStyle(c).verticalAlign==='middle')})()`, nil)); e != nil {
			t.Fatalf("Compact table does not fit viewport %d: %v", width, e)
		}
	}
	if e := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1100)); e != nil {
		t.Fatal(e)
	}

	for _, label := range []string{"价格 USD", "子体销量", "预计单件利润"} {
		if e := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`[...document.querySelectorAll('[data-screening-table] th button')].find(b=>b.textContent.trim().startsWith(%q)).click()`, label), nil), chromedp.Poll(`document.querySelectorAll('[data-screening-table] th[aria-sort=ascending]').length===1 && !document.querySelector('[data-screening-table] tbody').inert`, nil)); e != nil {
			t.Fatal(label, e)
		}
	}

	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{const handle=document.querySelector('button[aria-label="调整评分 / 评分数列宽"]');if(!handle)throw Error('Resize handle missing');const th=handle.closest('th');window.__resizeWidth=th.getBoundingClientRect().width;window.__resizeSort=th.getAttribute('aria-sort');const x=handle.getBoundingClientRect().left+4;handle.dispatchEvent(new MouseEvent('mousedown',{bubbles:true,clientX:x,clientY:th.getBoundingClientRect().top+10}));window.__resizeX=x;})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new MouseEvent('mousemove',{bubbles:true,clientX:window.__resizeX+80}))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new MouseEvent('mouseup',{bubbles:true,clientX:window.__resizeX+80}))`, nil),
		chromedp.Poll(`(()=>{const th=document.querySelector('button[aria-label="调整评分 / 评分数列宽"]').closest('th');return Math.abs(th.getBoundingClientRect().width-window.__resizeWidth-80)<2&&th.getAttribute('aria-sort')===window.__resizeSort})()`, nil),
		chromedp.Evaluate(`document.querySelector('button[aria-label="调整评分 / 评分数列宽"]').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`, nil),
		chromedp.Poll(`Math.abs(document.querySelector('button[aria-label="调整评分 / 评分数列宽"]').closest('th').getBoundingClientRect().width-window.__resizeWidth)<2`, nil),
	); e != nil {
		var diagnostic string
		chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({before:window.__resizeWidth,after:document.querySelector('button[aria-label="调整评分 / 评分数列宽"]')?.closest('th').getBoundingClientRect().width,sort:document.querySelector('button[aria-label="调整评分 / 评分数列宽"]')?.closest('th').getAttribute('aria-sort')})`, &diagnostic))
		t.Fatal("column dragging or reset failed", e, diagnostic)
	}

	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{const h=document.querySelector('button[aria-label="调整商品列宽"]');const x=h.getBoundingClientRect().left+4;window.__wideX=x;window.__baselineScrollWidth=document.querySelector('[data-screening-table]').scrollWidth;window.__navLeft=document.querySelector('nav[aria-label="初筛工作台"]').getBoundingClientRect().left;h.dispatchEvent(new MouseEvent('mousedown',{bubbles:true,clientX:x}));})()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new MouseEvent('mousemove',{bubbles:true,clientX:window.__wideX+800}))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new MouseEvent('mouseup',{bubbles:true,clientX:window.__wideX+800}))`, nil),
		chromedp.Poll(`(()=>{const a=document.querySelector('[data-screening-table]');return a.scrollWidth>a.clientWidth+100&&document.documentElement.scrollWidth<=innerWidth+2})()`, nil),
		chromedp.Evaluate(`(()=>{const a=document.querySelector('[data-screening-table]');a.scrollLeft=a.scrollWidth;if(a.scrollLeft<=0||Math.abs(document.querySelector('nav[aria-label="初筛工作台"]').getBoundingClientRect().left-window.__navLeft)>1||document.scrollingElement.scrollLeft!==0)throw Error('Table scrolling affected page layout');})()`, nil),
		chromedp.Evaluate(`document.querySelector('button[aria-label="调整商品列宽"]').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`, nil),
		chromedp.Poll(`(()=>{const a=document.querySelector('[data-screening-table]');return Math.abs(a.scrollWidth-window.__baselineScrollWidth)<=2&&[...a.querySelectorAll('th')].every(h=>getComputedStyle(h).textAlign==='center'&&[...h.querySelectorAll('span.min-w-0')].every(l=>l.getBoundingClientRect().height<=(parseFloat(getComputedStyle(l).lineHeight)||20)+2))&&[...a.querySelectorAll('tbody p')].every(p=>{const line=parseFloat(getComputedStyle(p).lineHeight);return getComputedStyle(p).textAlign===(p.closest('[data-fixed-align=left]')||p.getBoundingClientRect().height>line+2?'left':'center')})})()`, nil),
	); e != nil {
		t.Fatal("independent table scrolling or adaptive alignment", e)
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{const row=[...document.querySelectorAll('tbody tr')].find(r=>r.textContent.includes('BROWSER001'));row.querySelector('button[aria-controls^="family-"]').click()})()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('tr[data-family]')].some(r=>r.innerText.includes('GUI0000001'))`, nil),
	); e != nil {
		t.Fatalf("ASIN family expansion failed: %v", e)
	}
	if e := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{const rep=[...document.querySelectorAll('tbody>tr')].find(r=>r.innerText.includes('BROWSER001'));const child=document.querySelector('tr[data-family]');if(!document.querySelector('[data-screening-table]').innerText.includes('尺寸 / 重量')||!document.querySelector('[data-screening-table]').innerText.includes('卖家'))throw Error('new table headers missing');if(!child?.innerText.includes('22.86 × 7.62 × 17.78 cm')||!child.innerText.includes('154.22 g')||!child.innerText.includes('Fixture Seller'))throw Error('measurements or seller absent in peer row');if(!child||child.parentElement!==rep.parentElement||child.cells.length!==rep.cells.length||child.querySelector('table')||!child.querySelector('img')&&rep.querySelector('img')||!child.innerText.includes('查看详细数据')||!child.innerText.includes('测算'))throw Error('Family members must use full peer product rows')})()`, nil)); e != nil {
		t.Fatal(e)
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{if(performance.getEntriesByType('resource').some(x=>x.name.includes('/monthly?')))throw Error('history fetched before expansion');const row=[...document.querySelectorAll('tbody tr')].find(r=>r.textContent.includes('BROWSER001'));[...row.querySelectorAll('button')].find(b=>b.textContent.trim()==='查看详细数据').click()})()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-operating-trends] svg') && document.body.innerText.includes('近一年经营数据') && performance.getEntriesByType('resource').some(x=>x.name.includes('recent_months=12'))`, nil),
	); e != nil {
		t.Fatalf("annual data popover failed: %v", e)
	}

	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='查看来源文件').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[aria-label="来源原始行数据，可滚动"]')&&document.body.innerText.includes('BROWSER001')`, nil),
		chromedp.Evaluate(`(()=>{const table=document.querySelector('[aria-label="来源原始行数据，可滚动"]');if(!table.innerText.includes('ASIN')||!table.innerText.includes('BROWSER001')||!table.innerText.includes('manual kitchen tool'))throw Error('wrong raw source row');if(!document.querySelector('a[href*="/imports/"][href$="/source"]'))throw Error('source file link missing');if(document.body.innerText.includes('销量归属待确认'))throw Error('sales scope note remains');})()`, nil),
	); e != nil {
		t.Fatal("source row card failed", e)
	}
	// Resizing must keep both dialogs centered; only the content body scrolls.
	if e := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 480),
		chromedp.Poll(`(()=>{const dialogs=[...document.querySelectorAll('[data-slot="dialog-content"][data-open]')];return dialogs.length===2&&dialogs.every(d=>{if(d.getAnimations().some(a=>a.playState==='running'))return false;const r=d.getBoundingClientRect();return Math.abs(r.left+r.width/2-innerWidth/2)<2&&Math.abs(r.top+r.height/2-innerHeight/2)<2&&r.left>=8&&r.right<=innerWidth-8})})()`, nil),
		chromedp.Evaluate(`(()=>{const close=document.querySelector('button[aria-label="关闭来源原始数据"]');const panel=close.closest('[data-slot="dialog-content"]');const body=panel.querySelector('[data-data-dialog-body]');const before=close.getBoundingClientRect();body.scrollTop=body.scrollHeight;const after=close.getBoundingClientRect();if(Math.abs(before.top-after.top)>1||after.top<0||after.bottom>innerHeight)throw Error('Close button moved with content '+JSON.stringify({before:{top:before.top,bottom:before.bottom},after:{top:after.top,bottom:after.bottom},height:innerHeight,body:{client:body.clientHeight,scroll:body.scrollHeight},display:getComputedStyle(panel).display}));})()`, nil),
		chromedp.Click(`button[aria-label="关闭来源原始数据"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('[data-slot="dialog-content"][data-open]').length===1&&document.body.innerText.includes('近一年经营数据')`, nil),
		chromedp.KeyEvent("\u001b"),
		chromedp.Poll(`document.querySelectorAll('[data-slot="dialog-content"][data-open]').length===0`, nil),
		chromedp.EmulateViewport(1440, 1100),
		chromedp.Evaluate(`(()=>{const row=[...document.querySelectorAll('tbody tr')].find(r=>r.textContent.includes('BROWSER001'));[...row.querySelectorAll('button')].find(b=>b.textContent.trim()==='查看详细数据').click()})()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-operating-trends] svg')`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='查看来源文件').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[aria-label="来源原始行数据，可滚动"]')`, nil),
	); e != nil {
		t.Fatal("centered data dialog resize, fixed close or nested dismissal", e)
	}
	if e := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{if(!document.querySelector('button[aria-label="复制 ASIN BROWSER001"]'))throw Error('ASIN copy action missing');const t=document.querySelector('[data-screening-table]');if(!t||getComputedStyle(t.querySelector('thead')).position!=='sticky')throw Error('Product header must remain sticky');if(document.body.innerText.includes('Vercel'))throw Error('Template entry remains')})()`, nil)); e != nil {
		t.Fatal(e)
	}
	var screenshot []byte
	if e := chromedp.Run(ctx, chromedp.FullScreenshot(&screenshot, 80)); e != nil {
		t.Fatal(e)
	}
	dir := env("SCREENING_QA_DIR", os.TempDir()+"/screening-go-qa")
	os.MkdirAll(dir, 0750)
	if e := os.WriteFile(filepath.Join(dir, "screening-desktop.png"), screenshot, 0640); e != nil {
		t.Fatal(e)
	}
	checkBrowserGuidance(t, ctx, base)
}
