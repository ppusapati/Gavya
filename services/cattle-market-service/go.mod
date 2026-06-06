module github.com/ppusapati/gavya/services/cattle-market-service

go 1.26.1

require (
	connectrpc.com/connect v1.18.1
	github.com/jackc/pgx/v5 v5.7.6
	golang.org/x/net v0.40.0
	p9e.in/samavaya/packages v0.0.0
)

replace p9e.in/samavaya/packages => ../../pkg
