module github.com/ppusapati/gavya/services/shadow-settlement-service

go 1.26.1

require (
	connectrpc.com/connect v1.19.1
	github.com/jackc/pgx/v5 v5.7.6
	github.com/ppusapati/gavya/libs/integrity v0.0.0
	golang.org/x/net v0.40.0
	p9e.in/samavaya/packages v0.0.0
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
	golang.org/x/crypto v0.39.0 // indirect
	golang.org/x/sync v0.15.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace p9e.in/samavaya/packages => ../../pkg

replace github.com/ppusapati/gavya/libs/integrity => ../../libs/integrity
