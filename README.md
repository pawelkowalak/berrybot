[![CircleCI](https://circleci.com/gh/viru/berrybot.svg?style=svg)](https://circleci.com/gh/viru/berrybot)

Generate proto:

`protoc -I ./proto/ ./proto/steering.proto --go_out=plugins=grpc:proto`

Install and run mobile app locally:

`go install github.com/viru/berrybot && berrybot`

Install mobile app on connected device:

`gomobile install github.com/viru/berrybot/berrycli`

Install and run server:

`go install github.com/viru/berrybot/server && server`
