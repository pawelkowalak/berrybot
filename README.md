Generate proto:

`protoc -I ./proto/ ./proto/steering.proto --go_out=plugins=grpc:proto`

Install and run mobile app locally:

`go install github.com/viru/berrybot/berrycli && berrycli`

Install mobile app on connected device:

`gomobile install github.com/viru/berrybot/berrycli`

Install and run server:

`go install github.com/viru/berrybot/berry_server && berry_server`