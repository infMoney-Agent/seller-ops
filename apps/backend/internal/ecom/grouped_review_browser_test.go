package ecom

import (
	"context"
	"github.com/chromedp/cdproto/input"
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
	var resizePoint struct {
		X float64
		Y float64
	}
	if e := chromedp.Run(ctx, chromedp.Evaluate(seed, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); e != nil {
		t.Fatal("seed", e)
	}
	if e := chromedp.Run(ctx,
		chromedp.Reload(), chromedp.WaitReady(`#main-content`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__groupApi=async path=>{const r=await fetch('/api/ecom/screening/'+path);if(!r.ok)throw Error('API '+r.status);return await r.json()}`, nil),
		chromedp.Evaluate(`window.__reviewStep=1`, nil),
		chromedp.Poll(`!!document.querySelector('[data-screening-table] tbody input[type=checkbox]')`, nil),
		chromedp.Evaluate(`(()=>{const row=[...document.querySelectorAll('[data-screening-table] tbody tr')].find(r=>r.textContent.includes('GROUP00006'));row.querySelector('input[type=checkbox]').click();})()`, nil),
		chromedp.Evaluate(`window.__reviewStep=2`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='批量复核'&&!b.disabled)`, nil),
		chromedp.Evaluate(`window.__categoryStep='open-direct';[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='批量复核').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=3`, nil),
		chromedp.Poll(`!!document.querySelector('form[aria-label="已选商品批量复核"]')`, nil),
		chromedp.Evaluate(`window.__categoryStep='preview-direct';document.querySelector('form[aria-label="已选商品批量复核"] button[type=submit]').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=4`, nil),
		chromedp.Poll(`!!document.querySelector('section[aria-label="已选商品批量预览"]')`, nil),
		chromedp.Evaluate(`window.__reviewStep=5`, nil),
		chromedp.Poll(`!!document.querySelector('button[aria-label="关闭批量复核已选商品"]:not(:disabled)')`, nil),
		chromedp.Evaluate(`window.__categoryStep='close-direct';document.querySelector('button[aria-label="关闭批量复核已选商品"]').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=6`, nil),
		chromedp.Poll(`!document.querySelector('form[aria-label="已选商品批量复核"]')`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='清空').click()`, nil),
	); e != nil {
		var detail string
		chromedp.Run(ctx, chromedp.Evaluate(`window.__categoryStep+"\n"+document.body.innerText`, &detail))
		t.Fatal("direct review from library", e, detail)
	}

	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='分组复核').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=7`, nil),
		chromedp.Poll(`document.querySelector('[data-review-workspace]')&&document.querySelectorAll('aside[aria-label="人工参考样本"] article').length===2&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===6`, nil),

		chromedp.Evaluate(`if(document.querySelector('form[aria-label="批量复核设置"]')||document.body.innerText.includes('人工归类'))throw Error('Review form or categorize action remains before selection');if(document.querySelector('select[aria-label="类型组"]')||[...document.querySelectorAll('button')].some(b=>['前20组','后20组','下一类型组'].includes(b.textContent.trim())))throw Error('Type group controls remain')`, nil),
		chromedp.Evaluate(`window.__reviewStep=8`, nil),
		chromedp.Poll(`document.querySelector('nav[aria-label="商品分类目录"]')?.textContent.includes('Kitchen & Dining')&&document.querySelector('nav[aria-label="商品分类目录"]').getBoundingClientRect().right<=document.querySelector('[data-review-content]').getBoundingClientRect().left`, nil),

		chromedp.Evaluate(`(()=>{const tree=document.querySelector('nav[aria-label="商品分类目录"]');[...tree.querySelectorAll('button[aria-expanded]')].find(b=>b.getAttribute('aria-label').includes('Bath')).click();window.__categoryHeight=tree.querySelector('[data-review-category-list]').getBoundingClientRect().height;const handle=tree.querySelector('[data-category-resize]');handle.scrollIntoView({block:'center',behavior:'instant'});const r=handle.getBoundingClientRect();return {X:r.x+r.width/2,Y:r.y+r.height/2}})()`, &resizePoint),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if e := input.DispatchMouseEvent(input.MouseMoved, resizePoint.X, resizePoint.Y).Do(ctx); e != nil {
				return e
			}
			if e := input.DispatchMouseEvent(input.MousePressed, resizePoint.X, resizePoint.Y).WithButton(input.Left).WithButtons(1).WithClickCount(1).Do(ctx); e != nil {
				return e
			}
			if e := input.DispatchMouseEvent(input.MouseMoved, resizePoint.X, resizePoint.Y+90).WithButton(input.Left).WithButtons(1).Do(ctx); e != nil {
				return e
			}
			return input.DispatchMouseEvent(input.MouseReleased, resizePoint.X, resizePoint.Y+90).WithButton(input.Left).WithClickCount(1).Do(ctx)
		}),
		chromedp.Poll(`document.querySelector('[data-review-category-list]').getBoundingClientRect().height>window.__categoryHeight+60`, nil),
		chromedp.Evaluate(`(()=>{const tree=document.querySelector('nav[aria-label="商品分类目录"]');[...tree.querySelectorAll('button[aria-expanded]')].find(b=>b.getAttribute('aria-label').includes('Kitchen & Dining')).click()})()`, nil),
		chromedp.Evaluate(`window.__reviewStep=10`, nil),
		chromedp.Poll(`(()=>{const tree=document.querySelector('nav[aria-label="商品分类目录"]');const buttons=[...tree.querySelectorAll('button[aria-expanded]')];return buttons.find(b=>b.getAttribute('aria-label').includes('Bath')).getAttribute('aria-expanded')==='false'&&buttons.find(b=>b.getAttribute('aria-label').includes('Kitchen & Dining')).getAttribute('aria-expanded')==='true'})()`, nil),
		chromedp.Evaluate(`(()=>{const tree=document.querySelector('nav[aria-label="商品分类目录"]');[...tree.querySelectorAll('button')].find(b=>b.textContent.includes('Kitchen & Dining')).click();})()`, nil),
		chromedp.Evaluate(`window.__reviewStep=11`, nil),
		chromedp.Poll(`(()=>{const s=document.querySelector('nav[aria-label="商品分类目录"] button[aria-current="true"]');return s?.textContent.includes('Kitchen & Dining')&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===5})()`, nil),

		chromedp.Evaluate(`(()=>{const sidebar=document.querySelector('[data-review-category-sidebar]');if(sidebar.hidden)throw Error('Selection collapsed category tree');const refs=document.querySelector('aside[aria-label="人工参考样本"]'),candidates=document.querySelector('[aria-label="本类目候选商品"]');if(!sidebar.contains(refs)||refs.getBoundingClientRect().top<sidebar.querySelector('nav').getBoundingClientRect().bottom||candidates.getBoundingClientRect().left<refs.getBoundingClientRect().right)throw Error('References must be below categories in left sidebar');const content=document.querySelector('[data-review-content]');window.__reviewWidth=content.getBoundingClientRect().width;[...sidebar.querySelectorAll('button')].find(b=>b.textContent==='收起分类').click()})()`, nil),
		chromedp.Evaluate(`window.__reviewStep=12`, nil),
		chromedp.Poll(`document.querySelector('[data-review-category-content]').hidden&&!document.querySelector('[data-review-category-sidebar]').hidden&&document.querySelector('aside[aria-label="人工参考样本"]').getBoundingClientRect().height>0&&document.querySelectorAll('aside[aria-label="人工参考样本"] article').length===2&&document.querySelector('[data-review-content]').getBoundingClientRect().width===window.__reviewWidth`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-review-category-sidebar] button')].find(b=>b.textContent==='展开分类').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=13`, nil),
		chromedp.Poll(`!document.querySelector('[data-review-category-content]').hidden&&document.querySelector('nav button[aria-current=true]').textContent.includes('Kitchen')&&document.querySelector('[data-review-category-list]').getBoundingClientRect().height>window.__categoryHeight+60`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-review-content] button')].find(b=>b.textContent==='上一组').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=14`, nil),
		chromedp.Poll(`document.querySelector('[data-review-category-title]').textContent.includes('Cloths')&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===1`, nil),
		chromedp.Evaluate(`if(![...document.querySelectorAll('[data-review-content] button')].find(b=>b.textContent==='上一组').disabled)throw Error('First group previous button enabled');[...document.querySelectorAll('[data-review-content] button')].find(b=>b.textContent==='下一组').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=15`, nil),
		chromedp.Poll(`document.querySelector('[data-review-category-title]').textContent.includes('Hooks')&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===5`, nil),
		chromedp.Evaluate(`if(![...document.querySelectorAll('[data-review-content] button')].find(b=>b.textContent==='下一组').disabled)throw Error('Last group next button enabled');[...document.querySelectorAll('[data-review-content] button')].find(b=>b.textContent==='上一组').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=16`, nil),
		chromedp.Poll(`document.querySelector('[data-review-category-title]').textContent.includes('Cloths')&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===1`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="商品分类目录"] button')].find(b=>b.textContent.includes('Kitchen & Dining')).click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=17`, nil),
		chromedp.Poll(`document.querySelector('[data-review-category-title]').textContent.includes('Kitchen & Dining')&&document.querySelectorAll('[aria-label="本类目候选商品"] article').length===5`, nil),
		chromedp.Evaluate(`(()=>{const refs=document.querySelector('aside[aria-label="人工参考样本"]');refs.querySelector('button').click();const compare=refs.querySelectorAll('input[aria-label^="比较样本"]');compare[0].click();compare[1].click();})()`, nil),
		chromedp.Evaluate(`window.__reviewStep=18`, nil),
		chromedp.Poll(`!!document.querySelector('section[aria-label="样本对比"]')`, nil),
		chromedp.Evaluate(`(()=>{const scroll=document.querySelector('[aria-label="本类目候选商品"]');if(scroll.tabIndex!==0)throw Error('Candidate scroll region must accept keyboard focus');scroll.focus();document.querySelector('input[aria-label="全选本页可处理商品"]').click()})()`, nil),
		chromedp.Evaluate(`window.__reviewStep=19`, nil),
		chromedp.Poll(`document.querySelector('[data-review-toolbar]').textContent.includes('已选 3 / 100 项')`, nil),
	); e != nil {
		var debug string
		chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({step:window.__reviewStep,height:document.querySelector("[data-review-category-list]")?.getBoundingClientRect().height,initial:window.__categoryHeight,tree:document.querySelector("nav[aria-label=商品分类目录]")?.innerText})`, &debug))
		t.Fatal("group cards and sample selection", e, debug)
	}
	var screenshot []byte
	if e := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-review-workspace]').scrollIntoView({block:'start'})`, nil), chromedp.FullScreenshot(&screenshot, 90)); e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "grouped-review-desktop.png"), screenshot, 0600)
	}
	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-review-toolbar] button').click()`, nil),
		chromedp.Poll(`!!document.querySelector('form[aria-label="已选商品批量复核"]')&&document.querySelector('select[aria-label="批量复核方式"]').value==='reference'`, nil),
		chromedp.Evaluate(`document.querySelector('form[aria-label="已选商品批量复核"] button[type=submit]').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=20`, nil),
		chromedp.Poll(`!!document.querySelector('section[aria-label="已选商品批量预览"]')`, nil),
		chromedp.Evaluate(`window.__reviewStep=21`, nil),
		chromedp.Poll(`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认保存本批修改');return b&&!b.disabled})()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='确认保存本批修改').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=22`, nil),
		chromedp.Poll(`(async()=>{const p=await window.__groupApi('products?show_merged=true');return p.items.filter(p=>['GROUP00003','GROUP00004','GROUP00005'].includes(p.asin)).every(p=>p.review_status!=='pending')})()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('summary')].find(s=>s.textContent.trim()==='批量操作历史').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=23`, nil),
		chromedp.Poll(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='撤销此批次'&&!b.disabled)`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='撤销此批次').click()`, nil),
		chromedp.Evaluate(`window.__reviewStep=24`, nil),
		chromedp.Poll(`(async()=>{const p=await window.__groupApi('products?show_merged=true');return p.items.filter(p=>['GROUP00003','GROUP00004','GROUP00005'].includes(p.asin)).every(p=>p.review_status==='pending')})()`, nil),
		chromedp.EmulateViewport(390, 844),
		chromedp.Evaluate(`window.__reviewStep=25`, nil),
		chromedp.Poll(`document.querySelector('[data-review-workspace]')&&document.documentElement.scrollWidth<=window.innerWidth+1`, nil),
		chromedp.Evaluate(`document.querySelector('[data-review-workspace]').scrollIntoView({block:'start'})`, nil), chromedp.FullScreenshot(&screenshot, 90),
	); e != nil {
		t.Fatal("preview, apply, undo or narrow viewport", e)
	}
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "grouped-review-mobile.png"), screenshot, 0600)
	}
	if e := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1100), chromedp.Evaluate(`(async()=>{
    const api='/api/ecom/screening/';const call=async(path,method='GET',body)=>{const r=await fetch(api+path,{method,headers:{'Idempotency-Key':crypto.randomUUID(),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined});const j=await r.json();if(!r.ok)throw Error(JSON.stringify(j));return j};
    const wait=async id=>{for(let i=0;i<150;i++){const j=await call('jobs/'+id);if(j.state==='failed')throw Error(JSON.stringify(j));if(['succeeded','partial'].includes(j.state))return j;await new Promise(r=>setTimeout(r,100));}throw Error('Import timeout')};
    const rows=['ASIN,商品标题,父ASIN,变体数,月销量,子体销量,价格($),类目路径'];for(let i=1;i<=82;i++)rows.push(['FAM'+String(i).padStart(7,'0'),'steel storage hook '+i,i<=61?'FAMPARENT1':'','61','5000',String(i*10),'19.99','Family Checks:Hooks'].join(','));
    const form=new FormData();form.set('file',new Blob([rows.join('\n')],{type:'text/csv'}),'Product-Tools-US-2026.08-review-family.csv');const r=await fetch(api+'imports',{method:'POST',headers:{'Idempotency-Key':crypto.randomUUID()},body:form});const u=await r.json();if(!r.ok)throw Error(JSON.stringify(u));await wait(u.job_id);const c=await call('imports/'+u.import_id+'/commit','POST');const done=await wait(c.job_id);if(done.result?.screen?.job_id)await wait(done.result.screen.job_id);
    const params=new URLSearchParams({category_path:JSON.stringify(['Family Checks'])});const v=await call('review-workspace?'+params);if(v.counts.products!==82||v.group.display_products!==22||v.items.length!==22)throw Error('Family grouping must happen before pagination: '+JSON.stringify(v.counts));
    const rep=v.items.find(p=>p.parent_asin==='FAMPARENT1');if(rep.asin!=='FAM0000061'||rep.family_count!==61)throw Error('Wrong representative');params.set('family_product_id',rep.id);const first=await call('review-workspace?'+params);params.set('member_page','2');const second=await call('review-workspace?'+params);if(first.items.length!==50||second.items.length!==11||new Set([...first.items,...second.items].map(p=>p.id)).size!==61)throw Error('Family pages duplicate or omit children');
    params.set('member_page','1');params.set('selected_ids',[rep.id,first.items[1].id].join(','));const scoped=await call('review-workspace?'+params);if(scoped.items.length!==2)throw Error('Expanded children escaped selection scope');return 'ok';})()`, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='刷新').click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('nav[aria-label="商品分类目录"] button')].some(b=>b.textContent.includes('Family Checks'))`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="商品分类目录"] button')].find(b=>b.textContent.includes('Family Checks')).click()`, nil),
		chromedp.Poll(`document.querySelector('[data-review-category-title]').textContent==='Family Checks'&&document.querySelectorAll('[aria-label="本类目候选商品"] [data-review-family]').length===22`, nil),
		chromedp.Evaluate(`document.querySelector('[aria-label="本类目候选商品"] button[aria-label="展开 FAMPARENT1 的子体"]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[aria-label="本类目候选商品"] article[data-review-product="FAM0000060"]')`, nil),
		chromedp.Evaluate(`(()=>{const parent=document.querySelector('article[data-review-product="FAM0000061"]'),child=document.querySelector('article[data-review-product="FAM0000060"]');const toggle=parent.querySelector('button[aria-label="收起 FAMPARENT1 的子体"]');if(!parent.hasAttribute('data-review-row')||!child.hasAttribute('data-review-row')||getComputedStyle(parent).borderRadius!=='0px'||getComputedStyle(child).paddingLeft!==getComputedStyle(parent).paddingLeft||toggle.parentElement!==parent.querySelector('h4').parentElement)throw Error('Family products must use equal compact rows with inline expansion')})()`, nil),

		chromedp.Evaluate(`document.querySelector('input[aria-label="全选本页可处理商品"]').click()`, nil),
		chromedp.Poll(`document.querySelector('[data-review-toolbar]').textContent.includes('已选 71 / 100 项')`, nil),
	); e != nil {
		t.Fatal("family pagination, expansion and explicit selection", e)
	}

	if e := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-review-workspace]').scrollIntoView({block:'start',behavior:'instant'});document.querySelector('[aria-label="本类目候选商品"]').scrollTop=0`, nil), chromedp.FullScreenshot(&screenshot, 90)); e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		if e := os.WriteFile(filepath.Join(dir, "review-family-rows.png"), screenshot, 0600); e != nil {
			t.Fatal(e)
		}
	}

	if e := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='商品库').click()`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-screening-table] tbody > tr').length===28`, nil),
		chromedp.Evaluate(`(()=>{const s=document.querySelector('#screening-page-size');s.value='10';s.dispatchEvent(new Event('change',{bubbles:true}))})()`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-screening-table] tbody > tr').length===10`, nil),
		chromedp.Evaluate(`window.__firstPageAsin=document.querySelector('[data-screening-table] tbody tr').innerText;[...document.querySelectorAll('[data-products-pagination] button')].find(b=>b.textContent.trim()==='下一页').click()`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-screening-table] tbody > tr').length===10&&document.querySelector('[data-screening-table] tbody tr').innerText!==window.__firstPageAsin`, nil),
		chromedp.Evaluate(`(()=>{const s=document.querySelector('#screening-page-size');s.value='20';s.dispatchEvent(new Event('change',{bubbles:true}))})()`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-screening-table] tbody > tr').length===20&&document.querySelector('[data-products-pagination]').innerText.includes('1 / 2')`, nil),
		chromedp.Evaluate(`(()=>{const s=document.querySelector('#screening-page-size');s.value='100';s.dispatchEvent(new Event('change',{bubbles:true}))})()`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-screening-table] tbody > tr').length===28&&[...document.querySelectorAll('[data-products-pagination] button')].find(b=>b.textContent.trim()==='下一页').disabled`, nil),
		chromedp.Evaluate(`document.querySelector('[data-screening-table] button[aria-label="展开 FAMPARENT1 的子体"]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-screening-table] tr[data-family]')`, nil),
		chromedp.Evaluate(`if(document.querySelector('input[aria-label="展示全部子体"]'))throw Error('Removed all-child switch remains');document.querySelector('[data-screening-table] button[aria-label="收起 FAMPARENT1 的子体"]').click()`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-screening-table] tbody > tr').length===28&&document.querySelector('[data-screening-table] button[aria-label="展开 FAMPARENT1 的子体"]').closest('tr').innerText.includes('FAM0000061')`, nil),
		chromedp.Evaluate(`(()=>{const s=document.querySelector('#screening-page-size');s.value='50';s.dispatchEvent(new Event('change',{bubbles:true}))})()`, nil),
		chromedp.Poll(`document.querySelector('#screening-page-size').value==='50'&&document.querySelectorAll('[data-screening-table] tbody > tr').length===28`, nil),
	); e != nil {
		var detail string
		chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({size:document.querySelector('#screening-page-size')?.value,rows:document.querySelectorAll('[data-screening-table] tbody > tr').length,all:document.querySelector('input[aria-label="展示全部子体"]')?.checked,footer:document.querySelector('[data-products-pagination]')?.innerText,representativeCaption:document.querySelector('[data-screening-table]')?.innerText.includes('销量代表')})`, &detail))
		t.Fatal("page sizes and mandatory parent folding", e, detail)
	}

}
