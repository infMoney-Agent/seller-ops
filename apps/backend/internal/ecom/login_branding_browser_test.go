package ecom

import (
	"context"
	"github.com/chromedp/chromedp"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserSellerLoginBranding(t *testing.T) {
	base := os.Getenv("SELLER_LOGIN_QA_URL")
	if base == "" {
		t.Skip("explicit live login UI URL required")
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.Flag("no-sandbox", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.WindowSize(1280, 1000))
	if binary := os.Getenv("CHROME_BIN"); binary != "" {
		options = append(options, chromedp.ExecPath(binary))
	}
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()
	ctx, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	ctx, timeout := context.WithTimeout(ctx, 45*time.Second)
	defer timeout()
	if e := chromedp.Run(ctx, chromedp.Navigate(base+"/auth/sign-in"), chromedp.Poll(`(()=>{const u=new URL(location.href);return u.pathname==='/login/oauth/authorize'&&u.searchParams.get('code_challenge_method')==='S256'&&!!u.searchParams.get('state')&&!!u.searchParams.get('nonce')&&document.body.innerText.includes('登录电商运营工作台')&&!!document.querySelector('input[type=password]')})()`, nil), chromedp.Poll(`document.title.includes('Seller Ops')&&[...document.images].some(i=>i.src.includes('/branding/seller-ops-logo')&&i.complete&&i.naturalWidth>0)`, nil), chromedp.Evaluate(`(()=>{if(document.body.innerText.includes('继续登录'))throw Error('Intermediate page remains');if([...document.images].some(i=>i.src.includes('casdoor-logo')))throw Error('Provider logo remains');if(!document.body.innerText.includes('登录工作台'))throw Error('Branded login action missing')})()`, nil)); e != nil {
		t.Fatal("login redirect and branding", e)
	}
	var shot []byte
	if e := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("SCREENING_QA_DIR"); dir != "" {
		if e := os.WriteFile(filepath.Join(dir, "seller-ops-login.png"), shot, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Poll(`document.documentElement.scrollWidth<=innerWidth+1&&!!document.querySelector('input[type=password]')`, nil)); e != nil {
		t.Fatal("narrow login layout", e)
	}
}
