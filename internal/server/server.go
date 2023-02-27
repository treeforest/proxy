package server

import (
	"io"
	"net/http"
)

// GRPCServer grpc 服务接口
type GRPCServer interface {
	DoRequest(proxyId, method, path string, header http.Header, r io.Reader) (http.Header, []byte, int)
	Upgrade(wsId int64, proxyId, path string, header http.Header) (http.Header, int)
	WsOnConnected(proxyId string, wsId int64) <-chan []byte
	WsSend(wsId int64, msg []byte) error
	WsClose(wsId int64) error
	Stop()
}

// HTTPServer http 服务接口
type HTTPServer interface {
	WsClose(wsId int64) error
}
