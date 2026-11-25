module seller-ops/server

go 1.25.0

require (
	github.com/coreos/go-oidc/v3 v3.14.1
	github.com/jackc/pgx/v5 v5.7.6
	golang.org/x/oauth2 v0.30.0
)

require (
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

require (
	connectrpc.com/connect v1.18.1
	google.golang.org/protobuf v1.36.6
	seller-ops/contracts v0.0.0
	seller-ops/rpccompat v0.0.0
)

replace seller-ops/contracts => ../../proto/gen/go

replace seller-ops/rpccompat => ../../packages/rpccompat
