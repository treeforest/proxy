package grpc

import (
	"context"
	"google.golang.org/grpc"
	"time"

	log "github.com/treeforest/logger"
)

// Unary 一元RPC拦截，做一些日志打印的工作
func Unary(logger log.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {

		defer func() {
			if err := recover(); err != nil {
				logger.Errorf("[Recovery] %+v", err)
			}
		}()

		start := time.Now()

		resp, err = handler(ctx, req)

		end := time.Now()
		latency := end.Sub(start)

		logger.Debugf("METHOD:%s | REQUEST:{%v} | REPLY:{%v} | ERR:{%v} | TIME:%d",
			info.FullMethod, req, resp, err, latency/time.Millisecond)

		return resp, err
	}
}
