#/bin/zsh
protoc --go_out=. --go_grpc_out=. \
--go_opt=paths=source_relative --go_grpc_opt=paths=source_relative jutsu/jutsu.proto
