package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/treeforest/proxy/internal/pb"
	"github.com/treeforest/proxy/internal/server"
	"github.com/treeforest/proxy/internal/server/conf"
	"github.com/treeforest/proxy/internal/server/dao"

	"github.com/pkg/errors"
	log "github.com/treeforest/logger"
	"github.com/treeforest/snowflake"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

func New(c *conf.Config, jwtManger *JWTManager, opts ...grpc.ServerOption) *Server {
	logger := log.NewStdLogger(log.WithPrefix("GRPC"), log.WithLevel(log.Level(c.Logger.Level)))

	if opts == nil {
		opts = make([]grpc.ServerOption, 0)
	}
	opts = append(opts, grpc.UnaryInterceptor(Unary(logger)))
	keepParams := grpc.KeepaliveParams(keepalive.ServerParameters{
		// 最大的链接空闲时间，若超过 30min 收到心跳或请求，则将链接关闭
		MaxConnectionIdle: time.Minute * 30,
		// 若超过 10min 无消息来往（链接空闲），就发送一个ping消息检查链接是否还存在
		Time: time.Minute * 10,
		// 若在 20s 时间内未收到 pong 消息，则主动断开链接
		Timeout: time.Second * 20,
		// MaxConnectionAge: 最长链接时间，超过该值将强制关闭链接，默认是无穷。在要求长连接的情况下，直接默认即可
		// MaxConnectionAgeGrace: 超过最长链接时间后的一个延长时间，默认是无穷。在要求长连接的情况下，直接默认即可
	})
	enforcePolicy := grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		// 要求客户端两次 ping 的间隔必须大于 2min，若小于 2min 则关闭链接。作用：防止恶意的ping消息。
		MinTime: time.Minute * 5,
		// 若为 false，那么在没有 active stream 流的情况下进行 ping，服务端将会返回 GoAway。同样，是为了防止恶意的ping
		// 若为 true，则在没有 active stream 流的情况下也允许 ping。
		PermitWithoutStream: true,
	})
	creds := grpcCreds(c.EnableTLS)

	s := &Server{
		logger:        logger,
		jwtManager:    jwtManger,
		connStore:     newConnStore(),
		stopping:      0,
		ps:            NewPubSub(),
		wsClientStore: newWsClientStore(),
		dao:           dao.New(c.BoltDBPath),
		baseUrl:       c.BaseURL,
	}

	grpcServer := grpc.NewServer(append(opts, keepParams, enforcePolicy, creds)...)
	pb.RegisterProxyServer(grpcServer, s)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", c.RpcAddress)
	if err != nil {
		panic(err)
	}

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()

	s.grpcServer = grpcServer
	return s
}

// Server grpc Server
type Server struct {
	logger        log.Logger        // 日志对象
	HTTPServer    server.HTTPServer // HTTP 服务回调接口
	grpcServer    *grpc.Server      // 当前 GRPC 服务
	jwtManager    *JWTManager       // JWT 管理器
	connStore     *connectionStore  // GRPC 双向流连接管理
	ps            *PubSub           // 发布订阅器
	wsClientStore *wsClientStore    // websocket 客户端映射管理
	dao           *dao.Dao          // 数据库管理对象
	baseUrl       string            // 对外暴露的 Base URL
	stopping      int32             // 关闭标志
}

var _ server.GRPCServer = &Server{}
var _ pb.ProxyServer = &Server{}

// ProxyStream 是一个双向流的 GRPC 接口，访问时需要带上合法的 token 才允许建立连接。
// 成功建立连接后，服务端就可以从 stream 流中发送和接收 ProxyMessage.
func (s *Server) ProxyStream(stream pb.Proxy_ProxyStreamServer) error {
	// 进行客户端认证
	proxyId, err := s.authorize(stream.Context())
	if err != nil {
		s.logger.Debugf("authorize | ERR:%v", err)
		return err
	}

	// 一个账号只能进行一个连接，否则会返回重复登录的错误
	if _, err = s.connStore.getConnection(proxyId); err != nil {
		s.logger.Debugf("getConnection | proxyId:%s | ERR:%v", proxyId, err)
	} else {
		s.logger.Debugf("重复登录 | proxyId:%s", proxyId)
		return errors.New("重复登录")
	}

	// 连接成功
	conn := s.connStore.onConnected(proxyId, stream)

	// 设置消息处理回调
	conn.handler = s.handleMsgFromClient

	s.logger.Debugf("Connected | proxyId:%s", proxyId)

	defer func() {
		// 移除连接
		s.connStore.closeByID(proxyId)

		// 关闭当前 stream 关联的 websocket 连接
		wsIds := s.wsClientStore.closeByProxyId(proxyId)
		for _, wsId := range wsIds {
			// 通知 HTTP 关闭当前 stream 关联的 websocket 连接
			_ = s.HTTPServer.WsClose(wsId)
		}

		s.logger.Debugf("Disconnected | proxyId:%s", proxyId)
	}()

	// 开始服务
	return conn.serviceConnection()
}

// handleMsgFromClient 处理从客户端接收到的消息
func (s *Server) handleMsgFromClient(msg *ReceivedMessage) {
	if msg == nil {
		return
	}

	switch msg.GetContent().(type) {
	case *pb.ProxyMessage_Response:
		s.handleResponse(msg)
	case *pb.ProxyMessage_UpgradeResp:
		s.handleUpgradeResp(msg)
	case *pb.ProxyMessage_WsMsg:
		s.handleWsMsg(msg)
	case *pb.ProxyMessage_WsClose:
		s.handleWsClose(msg)
	case *pb.ProxyMessage_Ack:
		s.handleAck(msg)
	}
}

func (s *Server) handleAck(msg *ReceivedMessage) {
	_ = s.ps.Publish(msg.Mid, msg)
}

func (s *Server) handleWsClose(msg *ReceivedMessage) {
	wsClose := msg.GetWsClose()

	c, exists := s.wsClientStore.getClient(wsClose.Id)
	if !exists {
		// 服务端不存在对应连接，不执行关闭操作，直接返回
		return
	}

	s.wsClientStore.closeById(c.id)
	_ = s.HTTPServer.WsClose(wsClose.Id)
}

func (s *Server) handleResponse(msg *ReceivedMessage) {
	_ = s.ps.Publish(msg.Mid, msg)
}

func (s *Server) handleUpgradeResp(msg *ReceivedMessage) {
	_ = s.ps.Publish(msg.Mid, msg)
}

func (s *Server) handleWsMsg(msg *ReceivedMessage) {
	wsMsg := msg.GetWsMsg()

	client, exists := s.wsClientStore.getClient(wsMsg.Id)
	if !exists {
		s.logger.Debugf("getClient | WsId:%d | ERR: not exist", wsMsg.Id)

		// 服务端不存在连接映射，通知客户端断开对应的 websocket 连接
		msg.Response(&pb.ProxyMessage{
			Mid: snowflake.Generate(),
			Content: &pb.ProxyMessage_WsClose{
				WsClose: &pb.WsClose{Id: wsMsg.Id},
			},
		})

		return
	}

	// 将 GRPC 层收到的 websocket 消息转发给对应的 websocket 连接
	client.send(wsMsg.Msg)
}

// Register 用户注册接口
func (s *Server) Register(ctx context.Context, req *pb.RegisterReq) (*pb.RegisterReply, error) {
	err := check(req.Account, req.Password)
	if err != nil {
		return &pb.RegisterReply{}, err
	}
	err = s.dao.Register(req.Account, hashPassword(req.Password))
	return &pb.RegisterReply{}, err
}

// Login 用户登录接口
func (s *Server) Login(ctx context.Context, req *pb.LoginReq) (*pb.LoginReply, error) {
	if err := check(req.Account, req.Password); err != nil {
		return &pb.LoginReply{}, err
	}

	if err := s.dao.Login(req.Account, hashPassword(req.Password)); err != nil {
		return &pb.LoginReply{}, err
	}

	// 判断是否重复登录
	_, err := s.connStore.getConnection(req.Account)
	if err == nil {
		return &pb.LoginReply{}, status.Error(codes.AlreadyExists, "重复登录")
	}

	// 根据账号信息生成 JWT 令牌
	token, err := s.jwtManager.Generate(req.Account)
	if err != nil {
		return &pb.LoginReply{}, err
	}

	return &pb.LoginReply{
		Token:       token,
		RestAddress: fmt.Sprintf("%s/%s", s.baseUrl, req.Account),
		WsAddress:   fmt.Sprintf("ws:%s/ws/%s", strings.TrimPrefix(s.baseUrl, "http:"), req.Account),
	}, err
}

// Ping 提供给客户端判断服务端状态的接口
func (s *Server) Ping(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}

// DoRequest 发送 HTTP request 并返回 HTTP response
func (s *Server) DoRequest(proxyId, method, path string, header http.Header, r io.Reader) (http.Header, []byte, int) {
	body, _ := ioutil.ReadAll(r)
	msg, err := s.SendWithResp(proxyId, &pb.ProxyMessage{
		Mid: snowflake.Generate(),
		Content: &pb.ProxyMessage_Request{
			Request: &pb.Request{
				Method: method,
				Path:   path,
				Header: pb.NewHeaderWithHttpHeader(header),
				Body:   body,
			},
		},
	})
	if err != nil {
		s.logger.Warnf("DoRequest | ERR:%v", err)
		return nil, nil, http.StatusBadRequest
	}

	resp := msg.GetResponse()
	if resp == nil {
		// 意外结果，关闭对应连接。
		s.logger.Error("DoRequest | ERR: not response")
		s.connStore.closeByID(proxyId)
		return nil, nil, http.StatusBadRequest
	}

	return resp.Header.ToHttpHeader(), resp.Body, int(resp.Code)
}

// Upgrade websocket连接消息
func (s *Server) Upgrade(wsId int64, proxyId, path string, header http.Header) (http.Header, int) {
	msg, err := s.SendWithResp(proxyId, &pb.ProxyMessage{
		Mid: snowflake.Generate(),
		Content: &pb.ProxyMessage_UpgradeReq{
			UpgradeReq: &pb.UpgradeReq{
				Id:     wsId,
				Path:   path,
				Header: pb.NewHeaderWithHttpHeader(header),
			},
		},
	})
	if err != nil {
		s.logger.Debugf("Upgrade error: %v", err)
		return nil, http.StatusBadRequest
	}

	resp := msg.GetUpgradeResp()
	if resp == nil {
		// 意外结果，关闭对应连接。
		s.logger.Error("Upgrade | ERR: not upgrade response")
		s.connStore.closeByID(proxyId)
		return nil, http.StatusBadRequest
	}

	return resp.Header.ToHttpHeader(), http.StatusOK
}

// WsOnConnected 告诉 server 有 websocket 连接到来，并返回一个接收 websocket 客户端消息的通道。
func (s *Server) WsOnConnected(proxyId string, wsId int64) <-chan []byte {
	c := s.wsClientStore.onConnected(proxyId, wsId)
	return c.outBuff
}

// WsSend 发送 websocket 消息
func (s *Server) WsSend(wsId int64, msg []byte) error {
	c, exists := s.wsClientStore.getClient(wsId)
	if !exists {
		return errors.New("not found")
	}

	err := s.Send(c.proxyId, &pb.ProxyMessage{
		Mid: snowflake.Generate(),
		Content: &pb.ProxyMessage_WsMsg{
			WsMsg: &pb.WsMessage{
				Id:  wsId,
				Msg: msg,
			},
		},
	})

	return err
}

// WsClose 关闭一个 websocket 客户端
func (s *Server) WsClose(wsId int64) error {
	c, exists := s.wsClientStore.getClient(wsId)
	if !exists {
		return errors.New("not found")
	}

	s.wsClientStore.closeById(c.id)

	// 通知客户端断开相应连接
	err := s.SendUntilAcked(c.proxyId, &pb.ProxyMessage{
		Mid: snowflake.Generate(),
		Content: &pb.ProxyMessage_WsClose{
			WsClose: &pb.WsClose{
				Id: c.id,
			},
		},
	})

	return err
}

// Send 发送消息，不需要等待响应
func (s *Server) Send(proxyId string, msg *pb.ProxyMessage) error {
	if s.isStopping() {
		return errors.New("stopping")
	}

	// 获取 proxyId 所对应的连接
	conn, err := s.connStore.getConnection(proxyId)
	if err != nil {
		s.connStore.closeByID(proxyId)
		return errors.WithStack(err)
	}

	// 发送消息
	conn.send(msg, func(err error) {
		s.logger.Warnf("send | ERR:%v", err)

		// 若消息发送失败，关闭对应连接（意外结果）
		s.connStore.closeByID(proxyId)

		// 发布发送失败的消息。当item为 nil 时，代表发送失败。
		_ = s.ps.Publish(msg.Mid, nil)
	})

	return nil
}

// SendWithResp 发送请求消息并等待响应消息
func (s *Server) SendWithResp(proxyId string, msg *pb.ProxyMessage) (*ReceivedMessage, error) {
	if err := s.Send(proxyId, msg); err != nil {
		return nil, errors.WithStack(err)
	}

	// 订阅响应消息
	m, err := s.ps.Subscribe(msg.Mid, calcTTL(msg.Size())).Listen()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if m == nil {
		// 这是一个发送失败的响应消息
		return nil, errors.New("send failed")
	}

	return m, nil
}

// SendUntilAcked 发送消息并等待确认
func (s *Server) SendUntilAcked(proxyId string, msg *pb.ProxyMessage) error {
	m, err := s.SendWithResp(proxyId, msg)
	if err != nil {
		return errors.WithStack(err)
	}

	ack := m.GetAck()
	if ack == nil {
		// 意外结果，关闭对应连接。
		s.logger.Error("SendUntilAcked | ERR: not ack msg")
		s.connStore.closeByID(proxyId)
		return errors.New("not ack msg")
	}

	if ack.Error != "" {
		s.logger.Debugf("SendUntilAcked | ack.Error:%v", ack.Error)
		return errors.New(ack.Error)
	}

	return nil
}

// authorize 认证客户端的 token
func (s *Server) authorize(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "metadata is not provided")
	}

	values := md["authorization"]
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "authorization token is not provided")
	}

	accessToken := values[0]
	claims, err := s.jwtManager.Verify(accessToken)
	if err != nil {
		return "", status.Errorf(codes.Unauthenticated, "access token is invalid: %v", err)
	}

	return claims.Id, nil
}

func (s *Server) isStopping() bool {
	return atomic.LoadInt32(&s.stopping) == int32(1)
}

func (s *Server) Stop() {
	if s.isStopping() {
		return
	}
	atomic.StoreInt32(&s.stopping, 1)
	s.connStore.shutdown()
	s.logger.Stop()
	s.grpcServer.GracefulStop()
}

// oneM 1M 数据大小
const oneM = 1024 * 1024

// calcTTL 根据数据长度计算等待时间。按照 1M/10s 耗时计算。
func calcTTL(dataLen int) time.Duration {
	baseTTL := time.Second * 10
	return (time.Duration(dataLen/oneM) + 1) * baseTTL
}

func check(id, password string) error {
	if strings.Contains(id, "/") {
		return errors.New("the id cannot contain '/'")
	}
	if len(id) == 0 {
		return errors.New("the length of 'id' must be greater than 0")
	}
	if len(id) > 50 {
		return errors.New("the length of 'id' must be lesser than 50")
	}
	if len(password) == 0 {
		return errors.New("the length of 'password' must be greater than 0")
	}
	if len(password) > 50 {
		return errors.New("the length of 'password' must be lesser than 50")
	}
	return nil
}

func hashPassword(password string) string {
	hashedPassword := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hashedPassword[:])
}
