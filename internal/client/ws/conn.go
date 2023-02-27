package ws

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type msgSending struct {
	msg   []byte
	onErr func(error)
}

type connection struct {
	id       int64
	conn     *websocket.Conn
	outBuff  chan *msgSending
	Handler  func([]byte)
	stopChan chan struct{}
	stopOnce sync.Once
}

func newConnection(id int64, conn *websocket.Conn) *connection {
	conn.SetReadLimit(1024)
	conn.SetPingHandler(nil)
	conn.SetPongHandler(nil)
	conn.SetCloseHandler(nil)

	c := &connection{
		id:       id,
		conn:     conn,
		outBuff:  make(chan *msgSending, 256),
		stopChan: make(chan struct{}, 1),
	}
	return c
}

func (c *connection) close() {
	c.stopOnce.Do(func() {
		close(c.stopChan)
	})
}

func (c *connection) Send(msg []byte, onErr func(error)) {
	m := &msgSending{
		msg:   msg,
		onErr: onErr,
	}

	select {
	case c.outBuff <- m:
	case <-c.stopChan:
	}
}

func (c *connection) ServiceConnection() error {
	errChan := make(chan error, 1)
	msgChan := make(chan []byte, 256)
	defer close(msgChan)

	go c.readPump(errChan, msgChan)

	go c.writePump()

	for {
		select {
		case <-c.stopChan:
			return nil
		case err := <-errChan:
			return err
		case msg := <-msgChan:
			c.Handler(msg)
		}
	}
}

func (c *connection) writePump() {
	const writeWait = time.Second * 20
	const pingPeriod = time.Second * 30

	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
	}()

	for {
		select {
		case m := <-c.outBuff:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				go m.onErr(err)
				return
			}

			if _, err = w.Write(m.msg); err != nil {
				go m.onErr(err)
				return
			}

			// 将缓冲区里面的消息一并发出去.
			//n := len(c.outBuff)
			//for i := 0; i < n; i++ {
			//	select {
			//	case m = <-c.outBuff:
			//		if _, err = w.Write(m.msg); err != nil {
			//			go m.onErr(err)
			//			return
			//		}
			//	case <-c.stopChan:
			//		return
			//	}
			//}

			if err = w.Close(); err != nil {
				go m.onErr(err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.stopChan:
			return
		}
	}
}

// 接收消息协程
func (c *connection) readPump(errChan chan error, msgChan chan []byte) {
	defer func() {
		recover()
	}() // msgsCh might be closed

	for {
		select {
		case <-c.stopChan:
			return
		default:
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			select {
			case msgChan <- message:
			case <-c.stopChan:
				return
			}
		}
	}
}
