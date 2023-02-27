package grpc

import (
	"github.com/pkg/errors"
	"sync"
)

// connectionStore 连接管理器
type connectionStore struct {
	sync.RWMutex
	isClosing    bool
	id2Conn      map[string]*connection
	shutdownOnce sync.Once
}

func newConnStore() *connectionStore {
	return &connectionStore{
		isClosing: false,
		id2Conn:   make(map[string]*connection),
	}
}

// getConnection 通过id获取一个连接
func (cs *connectionStore) getConnection(id string) (*connection, error) {
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

func (cs *connectionStore) connNum() int {
	cs.RLock()
	defer cs.RUnlock()
	return len(cs.id2Conn)
}

// shutdown 关闭所有连接
func (cs *connectionStore) shutdown() {
	cs.shutdownOnce.Do(func() {
		cs.Lock()
		cs.isClosing = true

		for _, conn := range cs.id2Conn {
			conn.close()
		}
		cs.id2Conn = make(map[string]*connection)

		cs.Unlock()
	})
}

// closeByID 关闭id对应的连接
func (cs *connectionStore) closeByID(id string) {
	cs.Lock()
	defer cs.Unlock()
	if conn, exists := cs.id2Conn[id]; exists {
		conn.close()
		delete(cs.id2Conn, conn.id)
	}
}

// onConnected 存储连接
func (cs *connectionStore) onConnected(id string, stream Stream) *connection {
	cs.Lock()
	defer cs.Unlock()

	// 保持最新连接，若存在旧的连接，则关闭旧的连接
	if c, exists := cs.id2Conn[id]; exists {
		c.close()
	}

	conn := newConnection(stream)
	conn.id = id
	cs.id2Conn[id] = conn

	return conn
}
