package grpc

type wsClient struct {
	proxyId string
	id      int64
	outBuff chan []byte
}

func newWsClient(proxyId string, wsId int64) *wsClient {
	return &wsClient{
		proxyId: proxyId,
		id:      wsId,
		outBuff: make(chan []byte, 256),
	}
}

func (c *wsClient) send(msg []byte) {
	c.outBuff <- msg
}
