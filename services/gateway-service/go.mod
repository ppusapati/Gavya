module github.com/ppusapati/gavya/services/gateway-service

go 1.26.6

require (
	golang.org/x/net v0.40.0
	p9e.in/samavaya/packages v0.0.0
)

require (
	connectrpc.com/connect v1.19.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/fatih/color v1.18.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ppusapati/gavya/libs/integrity v0.0.0
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
)

replace p9e.in/samavaya/packages => ../../pkg

replace github.com/ppusapati/gavya/libs/integrity => ../../libs/integrity
