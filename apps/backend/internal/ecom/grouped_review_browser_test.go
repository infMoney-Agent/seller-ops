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

func TestBrowserGroupedReview(t *testing.T) {
	base := os.Getenv("SCREENING_WEB_QA_URL")
	if base == "" {
		t.Skip("browser fixture required")
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
	ctx, timeout := context.WithTimeout(ctx, 90*time.Second)
	defer timeout()
	if e := chromedp.Run(ctx, chromedp.Navigate(base+"/dashboard/ecom/screening"), chromedp.WaitReady("#main-content", chromedp.ByQuery)); e != nil {
		t.Fatal(e)
	}
	seed := `(async()=>{
 const api='/api/ecom/screening/';async function call(path,method='GET',body){const r=await fetch(api+path,{method,headers:{'Idempotency-Key':crypto.randomUUID(),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined});const v=await r.json();if(!r.ok)throw Error(JSON.stringify(v));return v;}
 const wait=async id=>{for(let i=0;i<100;i++){const j=await call('jobs/'+id);if(j.state==='failed')throw Error(JSON.stringify(j));if(['succeeded','partial'].includes(j.state))return j;await new Promise(r=>setTimeout(r,100));}throw Error('Job timeout')};
 const rows=['ASIN,商品标题,父ASIN,变体数,月销量,子体销量,价格($),商品尺寸（单位换算）,商品主图,卖家ID,卖家所属地,小类目,类目路径'];
 for(let i=1;i<=6;i++){const title=i===6?'cotton cleaning cloth':'stainless steel storage hook '+['red','blue','black','white','green'][i-1];rows.push(['GROUP0000'+i,title,'PARENT000'+i,'1','800','200+','19.99','22 x 10 x 4 cm','https://m.media-amazon.com/images/I/51Bpnh7KZuL._AC_US600_.jpg','SELLER000'+i,'US',i===6?'Cleaning Cloths':'Utility Hooks',i===6?'Home & Kitchen:Bath:Cleaning:Cloths':'Home & Kitchen:Kitchen & Dining:Storage:Hooks'].join(','));}
 const f=new FormData();f.set('file',new Blob([rows.join('\n')],{type:'text/csv'}),'Product-Tools-US-2026.08-group-review.csv');f.set('source_type','sellersprite');f.set('period_type','calendar_month');const upload=await fetch(api+'imports',{method:'POST',headers:{'Idempotency-Key':crypto.randomUUID()},body:f});const u=await upload.json();if(!upload.ok)throw Error(JSON.stringify(u));await wait(u.job_id);const c=await call('imports/'+u.import_id+'/commit','POST');const done=await wait(c.job_id);if(done.result?.screen?.job_id)await wait(done.result.screen.job_id);
 const all=await call('products?show_merged=true');const products=all.items.filter(p=>p.asin.startsWith('GROUP'));if(products.length!==6)throw Error('Fixture import failed');
 for(let i=0;i<2;i++){const p=products.find(p=>p.asin==='GROUP0000'+(i+1));await call('products/'+p.id+'/review','PATCH',{expected_data_revision:p.data_revision,expected_review_revision:p.review_revision,status:i===0?'kept':'excluded',reason:'已核对材质和结构',note:'隔离分组测试样本'});}
 window.__groupApi=call;return 'ok';})()`
	var result string
	if e := chromedp.Run(ctx, chromedp.Evaluate(seed, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); e != nil {
		t.Fatal("seed", e)
	}
	if e := chromedp.Run(ctx,
		chromedp.Reload(), chromedp.WaitReady(`#main-content`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__groupApi=async path=>{const r=await fetch('/api/ecom/screening/'+path);if(!r.ok)throw Error('API '+r.status);return await r.json()}`, nil),
		chromedp.Poll(`!!document.querySelector('[data-screening-table] tbody input[type=checkbox]')`, nil),
		chromedp.Evaluate(`(()=>{const row=[...document.querySelectorAll('[data-screening-table] tbody tr')].find(r=>r.textContent.includes('GROUP00006'));row.querySelector('input[type=checkbox]').click();})()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='批量复核'&&!b.disabled)`, nil),
		chromedp.Evaluate(`window.__categoryStep='open-direct';[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='批量复核').click()`, nil),
		chromedp.Poll(`!!document.querySelector('form[aria-label="已选商品批量复核"]')`, nil),
		chromedp.Evaluate(`window.__categoryStep='preview-direct';document.querySelector('form[aria-label="已选商品批量复核"] button[type=submit]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('section[aria-label="已选商品批量预览"]')`, nil),
		chromedp.Poll(`!!document.querySelector('button[aria-label="关闭批量复核已选商品"]:not(:disabled)')`, nil),
		chromedp.Evaluate(`window.__categoryStep='close-direct';document.querySelector('button[aria-label="关闭批量复核已选商品"]').click()`, nil),
		chromedp.Poll(`!document.querySelector('form[aria-label="已选商品批量复核"]')`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='清空').click()`, nil),
	); e != nil {
		var detail string
		chromedp.Run(ctx, chromedp.Evaluate(`window.__categoryStep+"\n"+document.body.innerText`, &detail))
		t.Fatal("direct review from library", e, detail)
	}

	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='分组复核').click()`, nil),
		chromedp.Poll(`document.querySelector('[data-review-workspace]')&&document.querySelectorAll('aside[aria-label="人工参考样本"] article').length===2&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===6`, nil),

		chromedp.Evaluate(`if(document.querySelector('select[aria-label="类型组"]')||[...document.querySelectorAll('button')].some(b=>['前20组','后20组','下一类型组'].includes(b.textContent.trim())))throw Error('Type group controls remain')`, nil),
		chromedp.Poll(`document.querySelector('nav[aria-label="商品分类目录"]')?.textContent.includes('Kitchen & Dining')&&getComputedStyle(document.querySelector('[data-review-workspace]')).gridTemplateColumns.split(' ').length===2`, nil),
		chromedp.Evaluate(`(()=>{const tree=document.querySelector('nav[aria-label="商品分类目录"]');[...tree.querySelectorAll('button')].find(b=>b.textContent.includes('Kitchen & Dining')).click();})()`, nil),
		chromedp.Poll(`(()=>{const s=document.querySelector('nav[aria-label="商品分类目录"] button[aria-current="true"]');return s?.textContent.includes('Kitchen & Dining')&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===5})()`, nil),

		chromedp.Evaluate(`(()=>{const refs=document.querySelector('aside[aria-label="人工参考样本"]');refs.querySelector('button').click();const compare=refs.querySelectorAll('input[aria-label^="比较样本"]');compare[0].click();compare[1].click();})()`, nil),
		chromedp.Poll(`!!document.querySelector('section[aria-label="样本对比"]')`, nil),
		chromedp.Evaluate(`(()=>{const scroll=document.querySelector('[aria-label="本类目候选商品"]');if(scroll.tabIndex!==0)throw Error('Candidate scroll region must accept keyboard focus');scroll.focus();document.querySelector('input[aria-label="全选本页可处理商品"]').click()})()`, nil),
		chromedp.Poll(`document.querySelector('[data-review-toolbar]').textContent.includes('已选 3 / 100 项')`, nil),
	); e != nil {
		t.Fatal("group cards and sample selection", e)
	}
	var screenshot []byte
	if e := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-review-workspace]').scrollIntoView({block:'start'})`, nil), chromedp.FullScreenshot(&screenshot, 90)); e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "grouped-review-desktop.png"), screenshot, 0600)
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='预览批量复核').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-review-preview]')`, nil),
		chromedp.Poll(`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认保存本批修改');return b&&!b.disabled})()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认保存本批修改').click()`, nil),
		chromedp.Poll(`(async()=>{const p=await window.__groupApi('products?show_merged=true');return p.items.filter(p=>['GROUP00003','GROUP00004','GROUP00005'].includes(p.asin)).every(p=>p.review_status!=='pending')})()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('summary')].find(s=>s.textContent.trim()==='批量操作历史').click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='撤销此批次'&&!b.disabled)`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='撤销此批次').click()`, nil),
		chromedp.Poll(`(async()=>{const p=await window.__groupApi('products?show_merged=true');return p.items.filter(p=>['GROUP00003','GROUP00004','GROUP00005'].includes(p.asin)).every(p=>p.review_status==='pending')})()`, nil),
		chromedp.EmulateViewport(390, 844),
		chromedp.Poll(`document.querySelector('[data-review-workspace]')&&document.documentElement.scrollWidth<=window.innerWidth+1`, nil),
		chromedp.Evaluate(`document.querySelector('[data-review-workspace]').scrollIntoView({block:'start'})`, nil), chromedp.FullScreenshot(&screenshot, 90),
	); e != nil {
		t.Fatal("preview, apply, undo or narrow viewport", e)
	}
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "grouped-review-mobile.png"), screenshot, 0600)
	}
}
