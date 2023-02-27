package client

import (
	"sync"

	"github.com/treeforest/proxy/internal/pb"

	"github.com/pkg/errors"
	log "github.com/treeforest/logger"
)

// ReceivedMessage 对接收到的消息的封装
type ReceivedMessage struct {
	*pb.ProxyMessage
	conn *connection
}

func (m *ReceivedMessage) Response(msg *pb.ProxyMessage) {
	m.conn.send(msg, func(e error) {
		log.Errorf("Response failed: %v", e)
		m.conn.close()
	})
}

func (m *ReceivedMessage) Ack(err error) {
	ackMsg := &pb.ProxyMessage{
		Mid: m.Mid,
		Content: &pb.ProxyMessage_Ack{
			Ack: &pb.Acknowledgement{},
		},
	}
	if err != nil {
		ackMsg.GetAck().Error = err.Error()
	}
	m.Response(ackMsg)
}

// msgSending 正在发送的消息
type msgSending struct {
	msg   *pb.ProxyMessage // 消息对象
	onErr func(error)      // 错误回调函数
}

func newConnection(stream pb.Proxy_ProxyStreamClient, handler func(m *ReceivedMessage)) *connection {
	return &connection{
		outBuff:  make(chan *msgSending, 256),
		stream:   stream,
		stopChan: make(chan struct{}, 1),
		handler:  handler,
	}
}

// connection 客户端连接管理对象
type connection struct {
	sync.RWMutex
	outBuff  chan *msgSending           // 发送通道
	handler  func(m *ReceivedMessage)   // 消息回调函数
	stream   pb.Proxy_ProxyStreamClient // 流对象
	stopChan chan struct{}
	stopOnce sync.Once
}

// close 关闭当前连接
func (conn *connection) close() {
	conn.stopOnce.Do(func() {
		close(conn.stopChan)
	})
}

// send 发送消息
func (conn *connection) send(msg *pb.ProxyMessage, onErr func(error)) {
	m := &msgSending{
		msg:   msg,
		onErr: onErr,
	}

	select {
	case conn.outBuff <- m:
	case <-conn.stopChan:
	}
}

// serviceConnection 启动当前连接的读写服务
func (conn *connection) serviceConnection() error {
	errChan := make(chan error, 1)
	msgChan := make(chan *ReceivedMessage, 256)
	defer close(msgChan)

	go conn.readFromStream(errChan, msgChan)

	go conn.writeToStream()

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

// writeToStream 写操作
func (conn *connection) writeToStream() {
	stream := conn.stream
	for {
		select {
		case m := <-conn.outBuff:
			err := stream.Send(m.msg)
			if err != nil {
				go m.onErr(errors.WithStack(err))
				return
			}

		case <-conn.stopChan:
			return
		}
	}
}

// readFromStream 读操作
func (conn *connection) readFromStream(errChan chan error, msgChan chan *ReceivedMessage) {
	defer func() {
		recover()
	}() // msgsCh might be closed

	stream := conn.stream

	for {
		select {
		case <-conn.stopChan:
			return
		default:
			msg, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}
			select {
			case msgChan <- &ReceivedMessage{ProxyMessage: msg, conn: conn}:
			case <-conn.stopChan:
				return
			}
		}
	}
}
