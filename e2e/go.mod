module github.com/ppusapati/gavya/e2e

go 1.26.1

require github.com/ppusapati/gavya/libs/integrity v0.0.0

require (
	connectrpc.com/connect v1.19.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/ppusapati/gavya/libs/integrity => ../libs/integrity

replace p9e.in/samavaya/packages => ../pkg
