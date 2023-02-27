package grpc

import (
	"github.com/stretchr/testify/require"
	"github.com/treeforest/proxy/internal/pb"
	"testing"
	"time"
)

func Test_PubSub(t *testing.T) {
	topic := int64(101)

	ps := NewPubSub()

	time.AfterFunc(time.Millisecond*500, func() {
		err := ps.Publish(topic, &pb.Response{})
		require.NoError(t, err)
	})

	sub := ps.Subscribe(topic, time.Second)

	_, err := sub.Listen()
	require.NoError(t, err)
}
