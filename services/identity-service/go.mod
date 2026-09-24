module github.com/ppusapati/gavya/services/identity-service

go 1.26.6

replace p9e.in/samavaya/packages => ../../pkg

replace github.com/ppusapati/gavya/libs/integrity => ../../libs/integrity

require (
	connectrpc.com/connect v1.20.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/ppusapati/gavya/libs/integrity v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.58.0
	p9e.in/samavaya/packages v0.0.0-00010101000000-000000000000
)

require (
	github.com/fatih/color v1.18.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
