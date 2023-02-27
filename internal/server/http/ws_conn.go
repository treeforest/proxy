package http

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	HandshakeTimeout: time.Second * 10,
	ReadBufferSize:   1024,
	WriteBufferSize:  1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func newWsConnection(proxyId string, id int64, conn *websocket.Conn) *wsConnection {
	conn.SetReadLimit(1024)
	conn.SetPingHandler(nil)
	conn.SetPongHandler(nil)
	conn.SetCloseHandler(nil)

	return &wsConnection{
		proxyId:  proxyId,
		id:       id,
		conn:     conn,
		stopChan: make(chan struct{}, 1),
	}
}

// wsConnection is a middleman between the ws wsConnection and the hub.
type wsConnection struct {
	proxyId  string          // 标识属于哪个代理客户端
	id       int64           // 连接的唯一标识
	conn     *websocket.Conn // ws 连接
	outBuff  <-chan []byte   // 发送通道
	handler  func([]byte)    // 消息处理回调
	stopChan chan struct{}
	stopOnce sync.Once
}

func (conn *wsConnection) close() {
	conn.stopOnce.Do(func() {
		close(conn.stopChan)
	})
}

func (conn *wsConnection) serviceConnection() error {
	errChan := make(chan error, 1)
	msgChan := make(chan []byte, 256)
	defer close(msgChan)

	go conn.readPump(errChan, msgChan)

	go conn.writePump(errChan)

	for {
		select {
		case <-conn.stopChan:
			return nil
		case err := <-errChan:
			return err
		case msg := <-msgChan:
			conn.handler(msg)
		}
	}
}

// 接收消息协程
func (conn *wsConnection) readPump(errChan chan error, msgChan chan []byte) {
	defer func() {
		recover()
	}() // msgsCh might be closed

	for {
		select {
		case <-conn.stopChan:
			return
		default:
			_, message, err := conn.conn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			select {
			case msgChan <- message:
			case <-conn.stopChan:
				return
			}
		}
	}
}

// 发送协程
func (conn *wsConnection) writePump(errChan chan<- error) {
	const writeWait = time.Second * 20
	const pingPeriod = time.Second * 30

	ticker := time.NewTicker(pingPeriod)
	defer func() {
		recover() // outBuff might be closed
		ticker.Stop()
	}()

	for {
		select {
		case message := <-conn.outBuff:
			_ = conn.conn.SetWriteDeadline(time.Now().Add(writeWait))

			w, err := conn.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				errChan <- err
				return
			}

			if _, err = w.Write(message); err != nil {
				errChan <- err
				return
			}

			if err = w.Close(); err != nil {
				errChan <- err
				return
			}

		case <-ticker.C:
			_ = conn.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				errChan <- err
				return
			}

		case <-conn.stopChan:
			return
		}
	}
}
