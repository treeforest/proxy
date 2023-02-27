package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/treeforest/proxy/internal/client/conf"
	"github.com/treeforest/proxy/internal/client/rest"
	"github.com/treeforest/proxy/internal/client/ws"
	"github.com/treeforest/proxy/internal/pb"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	log "github.com/treeforest/logger"
	"github.com/treeforest/snowflake"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Client is a proxy client
type Client struct {
	sync.RWMutex
	config      *conf.Config        // 配置文件
	conn        *connection         // 客户端双向流连接对象
	cc          *grpc.ClientConn    // grpc 客户端连接
	client      pb.ProxyClient      // grpc proxy 客户端
	restClient  *rest.Client        // REST 客户端，用于代理转发 HTTP 请求
	wsConnStore *ws.ConnectionStore // websocket 客户端连接管理
}

// Dial 连接代理服务，若连接成功则返回连接后的一个客户端对象
func Dial(c *conf.Config) (*Client, error) {
	keepaliveParam := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		// 若超过 10min 无消息来往（链接空闲），就发送一个ping消息检查链接是否还存在
		Time: time.Minute * 10,
		// 若在 20s 时间内未收到ping的响应消息，则主动断开链接
		Timeout: time.Second * 20,
		// 没有 active conn 时也发送 ping
		PermitWithoutStream: true,
	})
	transportOption := withTransportOption(c.EnableTLS)

	cc, err := grpc.Dial(c.ServerAddress, transportOption, keepaliveParam)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	_, err = pb.NewProxyClient(cc).Ping(context.Background(), &pb.Empty{})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	client := &Client{
		config:      c,
		cc:          cc,
		client:      pb.NewProxyClient(cc),
		restClient:  rest.NewClient(c.HttpBaseURL),
		wsConnStore: ws.NewConnStore(),
	}

	return client, nil
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.close()
	}
	if c.cc != nil {
		_ = c.cc.Close()
	}
}

// Register 注册
func (c *Client) Register(account, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()
	_, err := c.client.Register(ctx, &pb.RegisterReq{Account: account, Password: password})
	return err
}

// Login 登录
func (c *Client) Login(account, password string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()

	reply, err := c.client.Login(ctx, &pb.LoginReq{Account: account, Password: password})
	if err != nil {
		return "", "", errors.WithStack(err)
	}

	// 启动本地代理服务
	if err = c.serve(reply.Token); err != nil {
		return "", "", errors.WithStack(err)
	}

	return reply.RestAddress, reply.WsAddress, err
}

func (c *Client) serve(token string) error {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", token))
	stream, err := c.client.ProxyStream(ctx)
	if err != nil {
		return errors.WithStack(err)
	}

	c.conn = newConnection(stream, c.handleMsgFromServer)

	go func() {
		if err = c.conn.serviceConnection(); err != nil {
			c.conn.close()
			log.Fatalf("[GRPC] serviceConnection | ERR:%v", err)
		}
		log.Info("[GRPC] Stopped.")
	}()

	return nil
}

// handleMsgFromServer handle message from proxy server
func (c *Client) handleMsgFromServer(msg *ReceivedMessage) {
	if msg == nil {
		return
	}

	switch msg.GetContent().(type) {
	case *pb.ProxyMessage_Request:
		c.handleRequest(msg)
	case *pb.ProxyMessage_UpgradeReq:
		c.handleUpgradeReq(msg)
	case *pb.ProxyMessage_WsMsg:
		c.handleWsMsg(msg)
	case *pb.ProxyMessage_WsClose:
		c.handleWsClose(msg)
	}
}

// handleWsClose 关闭websocket连接
func (c *Client) handleWsClose(msg *ReceivedMessage) {
	c.wsConnStore.CloseByID(msg.GetWsClose().Id)

	// 回复一个确认消息
	msg.Ack(nil)
}

// handleWsMsg 处理 ws 消息
func (c *Client) handleWsMsg(msg *ReceivedMessage) {
	wsMsg := msg.GetWsMsg()

	wsConn, err := c.wsConnStore.GetConnection(wsMsg.Id)
	if err != nil {
		log.Warnf("[WS] GetConnection | ERR:%v", err)

		// 代理客户端没有建立对应的websocket连接，即连接断开，所以接下来应该
		// 通知服务端在服务端侧断开对应的websocket连接。
		msg.Response(&pb.ProxyMessage{
			Mid: snowflake.Generate(),
			Content: &pb.ProxyMessage_WsClose{
				WsClose: &pb.WsClose{Id: wsMsg.Id},
			},
		})
		return
	}

	wsConn.Send(wsMsg.Msg, func(err error) {
		// 若发送失败，则关闭连接，释放资源
		c.wsConnStore.CloseByID(wsMsg.Id)

		// 通知服务端同时断开连接
		msg.Response(&pb.ProxyMessage{
			Mid: snowflake.Generate(),
			Content: &pb.ProxyMessage_WsClose{
				WsClose: &pb.WsClose{Id: wsMsg.Id},
			},
		})
	})
}

var dialer *websocket.Dialer

// handleUpgradeReq 处理 upgrade 请求消息
func (c *Client) handleUpgradeReq(msg *ReceivedMessage) {
	upgradeReq := msg.GetUpgradeReq()

	urlStr := fmt.Sprintf("%s%s", c.config.WsBaseURL, strings.TrimPrefix(upgradeReq.Path, "/"))

	log.Debugf("[WS] Upgrade | HEADER:%v | URL%v", upgradeReq.Header, urlStr)

	wsConn, resp, err := dialer.Dial(urlStr, upgradeReq.Header.ToHttpHeader())

	if err != nil {
		log.Debugf("[WS] Dial | ERR:%v", err)
		msg.Response(&pb.ProxyMessage{
			Mid: msg.Mid,
			Content: &pb.ProxyMessage_UpgradeResp{
				UpgradeResp: &pb.UpgradeResp{
					Id:     upgradeReq.Id,
					Header: pb.Header{Header: map[string]pb.Header_Value{}},
					Error:  err.Error(),
				},
			},
		})
		return
	}

	// 这里需要剔除一些代理连接产生的头信息，否则会导致服务端头信息的冗余，从而无法连接
	upgradeRespHeader := func(header http.Header) http.Header {
		delete(header, "Connection")
		delete(header, "Sec-Websocket-Accept")
		delete(header, "Upgrade")
		return header
	}

	msg.Response(&pb.ProxyMessage{
		Mid: msg.Mid,
		Content: &pb.ProxyMessage_UpgradeResp{
			UpgradeResp: &pb.UpgradeResp{
				Id:     upgradeReq.Id,
				Header: pb.NewHeaderWithHttpHeader(upgradeRespHeader(resp.Header)),
				Error:  "",
			},
		},
	})

	log.Infof("[WS] Connected | ID:%d | URL:%s", upgradeReq.Id, urlStr)

	conn := c.wsConnStore.OnConnected(upgradeReq.Id, wsConn)
	conn.Handler = func(message []byte) {
		c.conn.send(&pb.ProxyMessage{
			Mid: snowflake.Generate(),
			Content: &pb.ProxyMessage_WsMsg{
				WsMsg: &pb.WsMessage{
					Id:  upgradeReq.Id,
					Msg: message,
				},
			},
		}, func(err error) {
			log.Errorf("[GRPC] Send | ERR:%v", err)
			c.conn.close()
		})
	}

	go func() {
		defer func() {
			_ = wsConn.Close()

			// 通知服务端， websocket 客户端断开的消息
			c.conn.send(&pb.ProxyMessage{
				Mid: snowflake.Generate(),
				Content: &pb.ProxyMessage_WsClose{
					WsClose: &pb.WsClose{Id: upgradeReq.Id},
				},
			}, func(err error) {
				log.Errorf("[GRPC] Send | ERR:%v", err)
				c.conn.close()
			})

			log.Infof("[WS] Disconnected | ID:%d | URL:%s", upgradeReq.Id, urlStr)
		}()

		if err = conn.ServiceConnection(); err != nil {
			if websocket.IsUnexpectedCloseError(err) {
				log.Debugf("[WS] ServiceConnection | ERR:%v", err)
				return
			}
			log.Errorf("[WS] ServiceConnection | ERR:%v", err)
		}
	}()
}

// handleRequest 处理 HTTP 请求消息
func (c *Client) handleRequest(msg *ReceivedMessage) {
	req := msg.GetRequest()
	if req == nil {
		return
	}

	// log.Debugf("header: %v", req.Header)

	respHeader, statusCode, respBody, err := c.restClient.Proxy(req.Method, req.Path, req.Header.ToHttpHeader(), req.Body)
	if err != nil {
		log.Warnf("[REST] Proxy | ERR:%v", err)
		return
	}

	// 向代理服务发送响应消息
	msg.Response(&pb.ProxyMessage{
		Mid: msg.Mid,
		Content: &pb.ProxyMessage_Response{
			Response: &pb.Response{
				Code:   int32(statusCode),
				Header: pb.NewHeaderWithHttpHeader(respHeader),
				Body:   respBody,
			},
		},
	})
}
