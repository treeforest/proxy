package main

import (
	"flag"
	log "github.com/treeforest/logger"
	"github.com/treeforest/proxy/internal/server/conf"
	"github.com/treeforest/proxy/internal/server/grpc"
	"github.com/treeforest/proxy/internal/server/http"
	"github.com/treeforest/proxy/pkg/graceful"
	pprof "net/http"
	_ "net/http/pprof"
	"time"
)

func init() {
	go func() {
		panic(pprof.ListenAndServe("0.0.0.0:8088", nil))
	}()
}

const (
	secretKey = "1q2w3e4r5t6y7u8i9op0"
)

func startRPCServer(c *conf.Config, jwtManager *grpc.JWTManager) *grpc.Server {
	s := grpc.New(c, jwtManager)
	log.Infof("serve GRPC server at %s", c.RpcAddress)
	return s
}

func startRESTServer(c *conf.Config, grpcSrv *grpc.Server) *http.Server {
	s := http.New(c, grpcSrv)
	log.Infof("serve HTTP server at %s", c.HttpAddress)
	return s
}

func setLogger(c *conf.Logger) {
	lvl := log.Level(1 << c.Level)

	if c.Type == 1 {
		log.SetLevel(lvl)
		return
	}

	// file logger setting
	log.SetLogger(log.NewSyncFileLogger(c.Path, int64(c.Capacity), log.WithLevel(lvl), log.WithPrefix("proxy")))
}

func main() {
	configPath := flag.String("conf", "config.yaml", "the config path")
	flag.Parse()

	// 加载配置文件，并进行初始化
	c := conf.Load(*configPath)
	setLogger(&c.Logger)
	log.Infof("server config\n%s", c.String())

	// 启动rpc服务
	rpcServer := startRPCServer(c, grpc.NewJWTManager(secretKey, time.Hour*24*10))

	// 启动http服务
	httpServer := startRESTServer(c, rpcServer)

	rpcServer.HTTPServer = httpServer

	graceful.Shutdown(func() {
		httpServer.Close()
		rpcServer.Stop()
		log.Stop()
	})
}
