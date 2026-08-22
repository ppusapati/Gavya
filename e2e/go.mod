module github.com/ppusapati/gavya/e2e

go 1.26.1

require github.com/ppusapati/gavya/libs/integrity v0.0.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

require (
	connectrpc.com/connect v1.19.1 // indirect
	github.com/jackc/pgx/v5 v5.7.6
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/ppusapati/gavya/libs/integrity => ../libs/integrity

replace p9e.in/samavaya/packages => ../pkg
