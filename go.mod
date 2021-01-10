module github.com/taiyoh/sqsd

go 1.14

replace github.com/taiyoh/sqsd/actor => ./actor

require (
	github.com/AsynkronIT/protoactor-go v0.0.0-20201225111938-2a1b4e9f6793
	github.com/aws/aws-sdk-go v1.35.35
	github.com/caarlos0/env/v6 v6.4.0
	github.com/fukata/golang-stats-api-handler v1.0.0
	github.com/hashicorp/logutils v1.0.0
	github.com/joho/godotenv v1.3.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/taiyoh/sqsd/actor v0.0.0-00010101000000-000000000000
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
)
