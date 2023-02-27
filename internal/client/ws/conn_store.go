package ws

import (
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// ConnectionStore 连接管理器
type ConnectionStore struct {
	sync.RWMutex
	isClosing    bool
	id2Conn      map[int64]*connection
	shutdownOnce sync.Once
}

func NewConnStore() *ConnectionStore {
	return &ConnectionStore{
		isClosing: false,
		id2Conn:   make(map[int64]*connection),
	}
}

// GetConnection 通过id获取一个连接
func (cs *ConnectionStore) GetConnection(id int64) (*connection, error) {
	cs.RLock()
	isClosing := cs.isClosing
	cs.RUnlock()

	if isClosing {
		return nil, errors.New("shutting down")
	}

	cs.RLock()
	conn, exists := cs.id2Conn[id]
	if exists {
		cs.RUnlock()
		return conn, nil
	}
	cs.RUnlock()

	return nil, errors.New("not found")
}

// Shutdown 关闭所有连接
func (cs *ConnectionStore) Shutdown() {
	cs.shutdownOnce.Do(func() {
		cs.Lock()
		cs.isClosing = true

		for _, conn := range cs.id2Conn {
			conn.close()
		}
		cs.id2Conn = make(map[int64]*connection)

		cs.Unlock()
	})
}

// CloseByID 关闭id对应的连接
func (cs *ConnectionStore) CloseByID(id int64) {
	cs.Lock()
	defer cs.Unlock()
	if conn, exists := cs.id2Conn[id]; exists {
		conn.close()
		delete(cs.id2Conn, conn.id)
	}
}

// OnConnected 存储连接
func (cs *ConnectionStore) OnConnected(id int64, wsConn *websocket.Conn) *connection {
	cs.Lock()
	defer cs.Unlock()

	// 保持最新连接，若存在旧的连接，则关闭旧的连接
	if c, exists := cs.id2Conn[id]; exists {
		c.close()
	}

	conn := newConnection(id, wsConn)
	cs.id2Conn[id] = conn

	return conn
}
