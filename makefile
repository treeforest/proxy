server:
	go run cmd/server/main.go -conf="cmd/server/config.yaml"

client:
	go run cmd/client/main.go -conf="cmd/client/config.yaml"

http:
	go run cmd/http/main.go