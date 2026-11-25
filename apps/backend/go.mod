module seller-ops/backend

go 1.25.0

require (
	github.com/PuerkitoBio/goquery v1.10.3
	github.com/chromedp/cdproto v0.0.0-20250724212937-08a3db8b4327
	github.com/chromedp/chromedp v0.14.2
	github.com/dlclark/regexp2 v1.11.5
	github.com/jackc/pgx/v5 v5.7.6
	github.com/redis/go-redis/v9 v9.12.1
	github.com/xuri/excelize/v2 v2.9.1
	golang.org/x/sync v0.14.0
	golang.org/x/text v0.25.0
)

require (
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-json-experiment/json v0.0.0-20250725192818-e39067aee2d2 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/richardlehane/mscfb v1.0.4 // indirect
	github.com/richardlehane/msoleps v1.0.4 // indirect
	github.com/tiendc/go-deepcopy v1.6.0 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.1 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
)

require (
	connectrpc.com/connect v1.18.1
	google.golang.org/protobuf v1.36.6
	seller-ops/contracts v0.0.0
)

replace seller-ops/contracts => ../../gen/go
