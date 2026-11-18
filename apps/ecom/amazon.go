package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var categories = map[string]string{"electronics": "electronics", "home": "kitchen", "sports": "sporting-goods", "toys": "toys-and-games", "beauty": "beauty", "health": "hpc", "clothing": "apparel", "pet": "pet-supplies", "tools": "hi", "office": "office-products"}
var domains = map[string]string{"ATVPDKIKX0DER": "amazon.com", "A1F83G8C2ARO7P": "amazon.co.uk", "A1PA6795UKMFR9": "amazon.de", "A13V1IB3VIYZZH": "amazon.fr", "A1RKKUPIHCS9HS": "amazon.es", "APJ6JRA9NG5V4": "amazon.it", "A1VC38T7YXB528": "amazon.co.jp"}

type Amazon struct {
	marketplace, base string
	client            *http.Client
	load              func(context.Context, string) (string, error)
	tokenMu           sync.Mutex
	adsToken          string
	adsExpiry         time.Time
}

func newAmazon() *Amazon {
	market := env("AMAZON_MARKETPLACE_ID", "ATVPDKIKX0DER")
	domain := domains[market]
	if domain == "" {
		domain = "amazon.com"
	}
	a := &Amazon{marketplace: market, base: "https://www." + domain, client: &http.Client{Timeout: 30 * time.Second}}
	a.load = browserHTML
	return a
}
func browserHTML(ctx context.Context, target string) (string, error) {
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.Flag("no-sandbox", true), chromedp.Flag("disable-dev-shm-usage", true), chromedp.Flag("lang", "en-US"))
	if proxy := osEnv("SCRAPER_PROXY_URL"); proxy != "" {
		options = append(options, chromedp.ProxyServer(proxy))
	}
	if binary := osEnv("CHROME_BIN"); binary != "" {
		options = append(options, chromedp.ExecPath(binary))
	}
	var alloc context.Context
	var cancel context.CancelFunc
	if remote := osEnv("SCRAPER_BROWSER_URL"); remote != "" {
		alloc, cancel = chromedp.NewRemoteAllocator(ctx, remote)
	} else {
		alloc, cancel = chromedp.NewExecAllocator(ctx, options...)
	}
	defer cancel()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	tab, timeout := context.WithTimeout(tab, 40*time.Second)
	defer timeout()
	var html string
	e := chromedp.Run(tab, chromedp.Navigate(target), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.OuterHTML("html", &html, chromedp.ByQuery))
	if e != nil {
		return "", e
	}
	if len(html) > 20*1024*1024 {
		return "", fmt.Errorf("scraped page too large")
	}
	lower := strings.ToLower(html)
	if strings.Contains(lower, "validatecaptcha") || strings.Contains(lower, "robot check") {
		return "", httpError{502, "Amazon要求验证，暂时无法采集"}
	}
	return html, nil
}
func doc(raw string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(raw))
}
func clean(s *goquery.Selection) string { return strings.Join(strings.Fields(s.First().Text()), " ") }

var numberRE = regexp.MustCompile(`[0-9][0-9,.]*`)
var asinLinkRE = regexp.MustCompile(`/dp/([A-Z0-9]{10})`)
var bsrRE = regexp.MustCompile(`#([0-9,]+)\s+in\s+([^\(]+)`)

func extractNumber(raw string) float64 {
	return number(numberRE.FindString(strings.ReplaceAll(raw, ",", "")))
}
func parseProduct(raw, asin, market string) (Product, error) {
	d, e := doc(raw)
	if e != nil {
		return Product{}, e
	}
	title := clean(d.Find("#productTitle"))
	if title == "" {
		return Product{}, httpError{502, "未读取到商品标题，页面可能要求登录或验证"}
	}
	p := Product{ASIN: asin, Title: title, Brand: clean(d.Find("#bylineInfo")), Price: extractNumber(clean(d.Find(".a-price .a-offscreen"))), Currency: "USD", ReviewCount: int(extractNumber(clean(d.Find("#acrCustomerReviewText")))), Rating: extractNumber(clean(d.Find(".a-icon-alt"))), Images: []string{}, Bullets: []string{}, Marketplace: market, Description: clean(d.Find("#productDescription"))}
	p.Brand = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(p.Brand, "Brand: "), "Visit the ", ""), " Store", ""))
	if m := bsrRE.FindStringSubmatch(clean(d.Find("#SalesRank, #detailBulletsWrapper_feature_div"))); len(m) > 2 {
		n, _ := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
		p.BSR = &n
		p.Category = strings.TrimSpace(m[2])
	}
	d.Find("#feature-bullets .a-list-item").Each(func(_ int, s *goquery.Selection) {
		if v := clean(s); v != "" {
			p.Bullets = append(p.Bullets, v)
		}
	})
	d.Find("#altImages img").Each(func(_ int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		if len(p.Images) < 6 && strings.HasPrefix(src, "https://") && !strings.Contains(src, "sprite") {
			p.Images = append(p.Images, strings.ReplaceAll(src, "._SS40_", "._SL500_"))
		}
	})
	return p, nil
}
func (a *Amazon) Product(ctx context.Context, asin string) (Product, error) {
	raw, e := a.load(ctx, a.base+"/dp/"+asin)
	if e != nil {
		return Product{}, e
	}
	return parseProduct(raw, asin, a.marketplace)
}
func parseBestSellers(raw, category, market string, limit int) ([]Product, error) {
	d, e := doc(raw)
	if e != nil {
		return nil, e
	}
	out := []Product{}
	seen := map[string]bool{}
	d.Find(".zg-grid-general-faceout, .zg-item-immersion").Each(func(_ int, s *goquery.Selection) {
		if len(out) >= limit {
			return
		}
		href, _ := s.Find("a[href*='/dp/']").First().Attr("href")
		m := asinLinkRE.FindStringSubmatch(href)
		if len(m) < 2 || seen[m[1]] {
			return
		}
		seen[m[1]] = true
		rank := int(extractNumber(clean(s.Find(".zg-bdg-text"))))
		title := clean(s.Find("._cDEzb_p13n-sc-css-line-clamp-3_g3dy1, .p13n-sc-truncate-desktop-type2"))
		if title == "" {
			title, _ = s.Find("img").First().Attr("alt")
		}
		out = append(out, Product{ASIN: m[1], Title: title, Price: extractNumber(clean(s.Find(".p13n-sc-price, ._cDEzb_p13n-sc-price_3mJ9Z, .a-price .a-offscreen"))), BSR: &rank, Category: category, Rating: extractNumber(clean(s.Find(".a-icon-alt"))), Currency: "USD", Images: []string{}, Bullets: []string{}, Marketplace: market})
	})
	if len(out) == 0 {
		return nil, httpError{502, "未读取到畅销榜商品，页面可能要求验证"}
	}
	return out, nil
}
func (a *Amazon) BestSellers(ctx context.Context, category string, limit int) ([]Product, error) {
	path, ok := categories[strings.ToLower(category)]
	if !ok {
		return nil, httpError{422, "不支持的商品类目"}
	}
	raw, e := a.load(ctx, "https://www.amazon.com/Best-Sellers/zgbs/"+path+"/")
	if e != nil {
		return nil, e
	}
	return parseBestSellers(raw, category, a.marketplace, limit)
}
func parseReviews(raw, asin string) ([]Review, error) {
	d, e := doc(raw)
	if e != nil {
		return nil, e
	}
	if d.Find("#ap_email, form[name='signIn']").Length() > 0 {
		return nil, httpError{502, "Amazon评论页要求登录"}
	}
	out := []Review{}
	d.Find("[data-hook='review']").Each(func(_ int, s *goquery.Selection) {
		id, _ := s.Attr("id")
		out = append(out, Review{ID: id, ASIN: asin, Rating: int(extractNumber(clean(s.Find("[data-hook='review-star-rating'] .a-icon-alt, [data-hook='cmps-review-star-rating'] .a-icon-alt")))), Title: clean(s.Find("[data-hook='review-title']")), Body: clean(s.Find("[data-hook='review-body']")), Author: clean(s.Find(".a-profile-name")), Date: clean(s.Find("[data-hook='review-date']")), Verified: s.Find("[data-hook='avp-badge']").Length() > 0, Helpful: int(extractNumber(clean(s.Find("[data-hook='helpful-vote-statement']"))))})
	})
	return out, nil
}
func (a *Amazon) Reviews(ctx context.Context, asin string, pages int) ([]Review, error) {
	out := []Review{}
	seen := map[string]bool{}
	for page := 1; page <= pages; page++ {
		raw, e := a.load(ctx, fmt.Sprintf("%s/product-reviews/%s?pageNumber=%d&sortBy=recent", a.base, asin, page))
		if e != nil {
			return nil, e
		}
		rows, e := parseReviews(raw, asin)
		if e != nil {
			return nil, e
		}
		if len(rows) == 0 {
			break
		}
		added := 0
		for _, r := range rows {
			key := r.ID
			if key == "" {
				key = r.Title + "|" + r.Body
			}
			if !seen[key] {
				out = append(out, r)
				seen[key] = true
				added++
			}
		}
		if added == 0 {
			break
		}
	}
	return out, nil
}
func (a *Amazon) adsAccessToken(ctx context.Context) (string, error) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	if time.Now().Add(time.Minute).Before(a.adsExpiry) {
		return a.adsToken, nil
	}
	id, secret, refresh := osEnv("AMAZON_ADS_CLIENT_ID"), osEnv("AMAZON_ADS_CLIENT_SECRET"), osEnv("AMAZON_ADS_REFRESH_TOKEN")
	if id == "" || secret == "" || refresh == "" {
		return "", httpError{503, "尚未配置广告API凭据"}
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}, "client_secret": {secret}}
	req, e := http.NewRequestWithContext(ctx, "POST", env("AMAZON_TOKEN_URL", "https://api.amazon.com/auth/o2/token"), strings.NewReader(form.Encode()))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r, e := a.client.Do(req)
	if e != nil {
		return "", e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", fmt.Errorf("Amazon token HTTP %d", r.StatusCode)
	}
	var token struct {
		Access  string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	if e = json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&token); e != nil {
		return "", e
	}
	if token.Access == "" || token.Expires < 1 {
		return "", fmt.Errorf("invalid Amazon token response")
	}
	a.adsToken = token.Access
	a.adsExpiry = time.Now().Add(time.Duration(token.Expires) * time.Second)
	return token.Access, nil
}
