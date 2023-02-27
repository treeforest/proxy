package http

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/treeforest/proxy/internal/server"
	"github.com/treeforest/proxy/internal/server/conf"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	log "github.com/treeforest/logger"
	"github.com/treeforest/snowflake"
)

// Server http server
type Server struct {
	logger      log.Logger
	srv         server.GRPCServer
	wsConnStore *wsConnectionStore
}

func New(c *conf.Config, srv server.GRPCServer) *Server {
	logger := log.NewStdLogger(log.WithPrefix("HTTP"), log.WithLevel(log.Level(c.Logger.Level)))

	s := &Server{
		logger:      logger,
		srv:         srv,
		wsConnStore: newWsConnStore(),
	}

	ln, err := net.Listen("tcp", c.HttpAddress)
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", s)

	go func() {
		if err = http.Serve(ln, mux); err != nil {
			panic(err)
		}
	}()

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			s.logger.Errorf("Recovery | ERR:%+v", err)
		}
	}()

	// 判断是否为 websocket 连接
	if strings.HasPrefix(r.URL.Path, "/ws") {
		s.serveWs(w, r)
		return
	}

	// 跨域
	origin := r.Header.Get("Origin") //请求头部
	if origin != "" {
		//接收客户端发送的origin （重要！）
		w.Header().Set("Access-Control-Allow-Origin", "*")
		//服务器支持的所有跨域请求的方法
		w.Header().Set("Access-Control-Allow-Methods", "*")
		//允许跨域设置可以返回其他子段，可以自定义字段
		w.Header().Set("Access-Control-Allow-Headers", "*")
		// 允许浏览器（客户端）可以解析的头部 （重要）
		w.Header().Set("Access-Control-Expose-Headers", "*")
		//设置缓存时间
		w.Header().Set("Access-Control-Max-Age", "172800")
		//允许客户端传递校验信息比如 cookie (重要)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	//允许类型校验
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok!"))
		return
	}

	// serve timer
	start := time.Now()
	path := r.URL.Path
	raw := r.URL.RawQuery
	method := r.Method
	addr := r.RemoteAddr

	// next
	s.handler(w, r)

	// Stop timer
	end := time.Now()
	latency := end.Sub(start)
	if raw != "" {
		path = path + "?" + raw
	}
	s.logger.Infof("METHOD:%s | PATH:%s | IP:%s | TIME:%d", method, path, addr, latency/time.Millisecond)
}

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	proxyID, path, err := s.parse(r.URL.String())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// s.logger.Debugf("request: %v", *r)
	// s.logger.Debugf("header:%v | proxyID:%s | path:%s | sourcePath:%s", r.Header, proxyID, path, r.URL.String())

	respHeader, respData, respStatusCode := s.srv.DoRequest(proxyID, r.Method, path, r.Header, r.Body)

	if respHeader != nil {
		for k, vv := range respHeader {
			for _, v := range vv {
				w.Header().Set(k, v)
			}
		}
	}

	w.WriteHeader(respStatusCode)

	if respData != nil {
		_, _ = w.Write(respData)
	}
}

// serveWs handles ws requests from the peer.
func (s *Server) serveWs(w http.ResponseWriter, r *http.Request) {
	proxyId, path, err := s.parse(strings.TrimPrefix(r.URL.String(), "/ws"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.logger.Debugf("[WS] serveWs | ProxyId:%s | PATH:%s", proxyId, path)

	// 为连接生成一个唯一的标识 ID
	wsId := snowflake.Generate()

	// 1.通知代理客户端进行 websocket 连接
	responseHeader, statusCode := s.srv.Upgrade(wsId, proxyId, path, wsRequestHeader(r.Header))
	if statusCode != http.StatusOK {
		w.WriteHeader(statusCode)
		s.logger.Debugf("[WS] Upgrade | CODE:%d", statusCode)
		return
	}

	s.logger.Debugf("[WS] Upgrade success | RespHeader:%v ", responseHeader)

	// 2.开始进行 websocket 连接
	wsConn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.logger.Warnf("[WS] Upgrade | ERR:%v", err)
		return
	}
	defer func() { _ = wsConn.Close() }()

	// 3.连接成功，开启读写服务
	outBuff := s.srv.WsOnConnected(proxyId, wsId)
	conn := s.wsConnStore.onConnected(proxyId, wsId, wsConn)
	conn.outBuff = outBuff
	conn.handler = func(message []byte) {
		if e := s.srv.WsSend(wsId, message); e != nil {
			s.logger.Warnf("[WS] WsSend | ERR:%v", e)
			s.wsConnStore.closeByWsId(wsId)
		}
	}

	s.logger.Debugf("[WS] Connected | ProxyId:%s | WsId:%d", proxyId, wsId)

	defer func() {
		_ = s.srv.WsClose(wsId)
		s.wsConnStore.closeByWsId(wsId)
		s.logger.Debugf("[WS] Disconnected | ProxyId:%s | WsId:%d", proxyId, wsId)
	}()

	if err = conn.serviceConnection(); err != nil {
		if websocket.IsUnexpectedCloseError(err) {
			s.logger.Debugf("[WS] Close | ERR:%v", err)
		} else {
			s.logger.Warnf("[WS] Close | ERR:%v", err)
		}
	}
}

func (s *Server) WsClose(wsId int64) error {
	s.wsConnStore.closeByWsId(wsId)
	return nil
}

func (s *Server) Close() {
	s.wsConnStore.shutdown()
	s.logger.Stop()
}

// parse 解析源url
func (s *Server) parse(srcUrl string) (string, string, error) {
	nodes := strings.Split(srcUrl, "/")

	if len(nodes) < 2 || (len(nodes) == 2 && nodes[1] == "") {
		return "", "", errors.New("illegal srcUrl")
	}

	for i := 2; i < len(nodes); i++ {
		if nodes[i] == "" {
			return "", "", errors.New("illegal url node")
		}
	}

	id := nodes[1]
	if id == "" {
		return "", "", errors.New("illegal id")
	}

	path := fmt.Sprintf("/%s", strings.Join(nodes[2:], "/"))
	return id, path, nil
}

func wsRequestHeader(header http.Header) http.Header {
	// 这些字段在Upgrade时自动生成，这里需要做过滤处理，不用透传
	delete(header, "Connection")
	delete(header, "Upgrade")
	delete(header, "Sec-Websocket-Version")
	delete(header, "Sec-Websocket-Extensions")
	delete(header, "Sec-Websocket-Key")
	delete(header, "User-Agent")
	delete(header, "Origin")

	return header
}
