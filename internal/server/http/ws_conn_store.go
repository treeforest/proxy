package http

import (
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// wsConnectionStore websocket 连接管理
type wsConnectionStore struct {
	sync.RWMutex
	isClosing    bool
	wsId2Conn    map[int64]*wsConnection
	shutdownOnce sync.Once
}

func newWsConnStore() *wsConnectionStore {
	return &wsConnectionStore{
		isClosing: false,
		wsId2Conn: make(map[int64]*wsConnection),
	}
}

// getConnection 通过 websocket id 获取一个连接
func (cs *wsConnectionStore) getConnection(id int64) (*wsConnection, error) {
	cs.RLock()
	isClosing := cs.isClosing
	cs.RUnlock()

	if isClosing {
		return nil, errors.New("shutting down")
	}

	cs.RLock()
	conn, exists := cs.wsId2Conn[id]
	if exists {
		cs.RUnlock()
		return conn, nil
	}
	cs.RUnlock()

	return nil, errors.New("not found")
}

// shutdown 关闭所有连接
func (cs *wsConnectionStore) shutdown() {
	cs.shutdownOnce.Do(func() {
		cs.Lock()
		cs.isClosing = true

		for _, conn := range cs.wsId2Conn {
			conn.close()
		}
		cs.wsId2Conn = make(map[int64]*wsConnection)

		cs.Unlock()
	})
}

// closeByWsId 关闭 websocket id 对应的连接
func (cs *wsConnectionStore) closeByWsId(id int64) {
	cs.Lock()
	defer cs.Unlock()
	if conn, exists := cs.wsId2Conn[id]; exists {
		conn.close()
		delete(cs.wsId2Conn, conn.id)
	}
}

// closeByProxyId 关闭 proxy id 对应的所有连接
func (cs *wsConnectionStore) closeByProxyId(proxyId string) {
	cs.Lock()
	defer cs.Unlock()

	wsIds := make([]int64, 0)
	for wsId, conn := range cs.wsId2Conn {
		if conn.proxyId != proxyId {
			continue
		}
		wsIds = append(wsIds, wsId)
	}

	for _, wsId := range wsIds {
		conn := cs.wsId2Conn[wsId]
		conn.close()
		delete(cs.wsId2Conn, wsId)
	}
}

// onConnected 存储连接
func (cs *wsConnectionStore) onConnected(proxyId string, wsId int64, wsConn *websocket.Conn) *wsConnection {
	cs.Lock()
	defer cs.Unlock()

	// 保持最新连接，若存在旧的连接，则关闭旧的连接
	if c, exists := cs.wsId2Conn[wsId]; exists {
		c.close()
	}

	conn := newWsConnection(proxyId, wsId, wsConn)
	cs.wsId2Conn[wsId] = conn

	return conn
}
