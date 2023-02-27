package grpc

import (
	"sync"
	"time"

	"github.com/pkg/errors"
)

// PubSub 单订阅者的发布订阅，类似事件通知。为提示性能，上层应用需要保证topic唯一性，该
// 实现中不对topic的唯一性进行检测。
type PubSub struct {
	sync.RWMutex
	pool sync.Pool

	// 订阅者
	subscriptions map[int64]*subscription
}

func NewPubSub() *PubSub {
	return &PubSub{
		pool: sync.Pool{New: func() any {
			return &subscription{c: make(chan *ReceivedMessage, 1)}
		}},
		subscriptions: make(map[int64]*subscription),
	}
}

type subscription struct {
	top int64 // topic
	ttl time.Duration
	c   chan *ReceivedMessage
}

// Listen 阻塞等待订阅的结果，超时或正常返回
func (s *subscription) Listen() (*ReceivedMessage, error) {
	select {
	case <-time.After(s.ttl):
		return nil, errors.New("timed out")
	case item := <-s.c:
		return item, nil
	}
}

// Publish 发布
func (ps *PubSub) Publish(topic int64, item *ReceivedMessage) error {
	ps.RLock()
	defer ps.RUnlock()

	sub, subscribed := ps.subscriptions[topic]
	if !subscribed {
		return errors.New("no subscribers")
	}

	sub.c <- item
	return nil
}

// Subscribe 订阅
func (ps *PubSub) Subscribe(topic int64, ttl time.Duration) *subscription {
	sub := ps.pool.Get().(*subscription)
	sub.top = topic
	sub.ttl = ttl

	ps.Lock()
	ps.subscriptions[topic] = sub
	ps.Unlock()

	time.AfterFunc(ttl, func() {
		ps.unSubscribe(sub)
	})

	return sub
}

// 取消订阅
func (ps *PubSub) unSubscribe(sub *subscription) {
	ps.Lock()
	delete(ps.subscriptions, sub.top)
	ps.Unlock()

	ps.pool.Put(sub)
}
