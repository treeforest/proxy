package grpc

import (
	"sync"
)

type wsClientStore struct {
	mux       sync.RWMutex
	id2client map[int64]*wsClient
}

func newWsClientStore() *wsClientStore {
	return &wsClientStore{
		id2client: make(map[int64]*wsClient),
	}
}

func (cs *wsClientStore) getClient(id int64) (*wsClient, bool) {
	cs.mux.RLock()
	defer cs.mux.RUnlock()
	c, exists := cs.id2client[id]
	return c, exists
}

func (cs *wsClientStore) closeById(id int64) {
	cs.mux.Lock()
	defer cs.mux.Unlock()
	delete(cs.id2client, id)
}

func (cs *wsClientStore) closeByProxyId(proxyId string) []int64 {
	cs.mux.Lock()
	defer cs.mux.Unlock()
	wsIds := make([]int64, 0)
	for wsId, c := range cs.id2client {
		if c.proxyId == proxyId {
			wsIds = append(wsIds, wsId)
		}
	}
	for _, wsId := range wsIds {
		delete(cs.id2client, wsId)
	}
	return wsIds
}

func (cs *wsClientStore) onConnected(proxyId string, wsId int64) *wsClient {
	cs.mux.Lock()
	defer cs.mux.Unlock()
	c := newWsClient(proxyId, wsId)
	cs.id2client[c.id] = c
	return c
}
