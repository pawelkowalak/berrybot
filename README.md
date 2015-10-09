Generate proto:

`protoc -I ./proto/ ./proto/steering.proto --go_out=plugins=grpc:proto`