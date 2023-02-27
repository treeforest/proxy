package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	log "github.com/treeforest/logger"
	"github.com/treeforest/proxy/internal/client"
	"github.com/treeforest/proxy/internal/client/conf"
	"github.com/treeforest/proxy/pkg/graceful"
	"golang.org/x/term"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"io/ioutil"
	// _ "net/rest/pprof"
	"os"
	"os/exec"
	"runtime"
)

func init() {
	//go func() {
	//	panic(rest.ListenAndServe(":8188", nil))
	//}()
}

const (
	clientCertFile   = "cert/client-cert.pem"
	clientKeyFile    = "cert/client-key.pem"
	clientCACertFile = "cert/ca-cert.pem"
)

func loadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := ioutil.ReadFile(clientCACertFile)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load server's certificate and private key
	clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	return credentials.NewTLS(config), nil
}

func withTransportOption(enableTLS bool) grpc.DialOption {
	transportOption := grpc.WithTransportCredentials(insecure.NewCredentials())

	if enableTLS {
		tlsCredentials, err := loadTLSCredentials()
		if err != nil {
			log.Fatal("cannot load TLS credentials: ", err)
		}

		transportOption = grpc.WithTransportCredentials(tlsCredentials)
	}

	return transportOption
}

func checkError(err error) {
	if err != nil {
		log.Errorf("%+v", err)
		os.Exit(1)
	}
}

// 控制台清屏
func clear() {
	var cmd *exec.Cmd
	if runtime.GOOS == `windows` {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

// 登录
func login(c *client.Client) (string, string) {
	for {
		fmt.Print("登录 or 注册?(1/2): ")
		var input string
		_, err := fmt.Scanf("%s\n", &input)
		checkError(err)

		switch input {
		case "", "1":
			{
				fmt.Println("\t[用户登录]")

				fmt.Print("账号: ")
				var account string
				_, err = fmt.Scanf("%s\n", &account)
				checkError(err)

				fmt.Print("密码: ")
				passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				checkError(err)
				fmt.Print("\n")

				if len(passwordBytes) == 0 {
					log.Error("密码不能为空")
					continue
				}

				restAddr, wsAddr, err := c.Login(account, string(passwordBytes))
				if err != nil {
					log.Errorf("登录失败，%v\n", err)
					continue
				}

				return restAddr, wsAddr
			}

		case "2":
			{
				fmt.Println("\t[用户注册]")

				fmt.Print("账号: ")
				var account string
				_, err = fmt.Scanf("%s\n", &account)
				checkError(err)

				fmt.Print("密码: ")
				passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				checkError(err)
				fmt.Print("\n")

				if len(passwordBytes) == 0 {
					log.Error("密码不能为空")
					continue
				}

				fmt.Print("确认密码: ")
				checkPasswordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				checkError(err)
				fmt.Print("\n")

				if !bytes.Equal(passwordBytes, checkPasswordBytes) {
					log.Error("两次输入的密码不一致，请重新输入！")
					continue
				}

				if err = c.Register(account, string(passwordBytes)); err != nil {
					log.Errorf("注册失败，%v\n", err)
					continue
				}

				log.Infof("账号 [%s] 注册成功！", account)
			}
		}
	}
}

func main() {
	clear()

	configPath := flag.String("conf", "config.yaml", "the cli config path")
	flag.Parse()

	log.SetLevel(log.DEBUG)

	config := conf.Load(*configPath)

	// 连接
	c, err := client.Dial(config)
	if err != nil {
		log.Fatalf("连接代理服务[%s]失败, %v\n", config.ServerAddress, err)
		return
	}

	log.Info("连接代理服务成功, 开始进行用户登录...")

	// 登录
	restAddr, wsAddr := login(c)

	clear()
	log.Info("登录成功 ^_^")
	log.Infof("HTTP 代理地址: %s -> %s", restAddr, config.HttpBaseURL)
	log.Infof("Websocket 代理: %s -> %s", wsAddr, config.WsBaseURL)
	//log.Infof("HTTP 代理地址：%s", config.HttpBaseURL)
	//log.Infof("HTTP 公网地址：%s", restAddr)
	//log.Infof("Websocket 代理地址: %s", config.WsBaseURL)
	//log.Infof("Websocket 公网地址: %s", wsAddr)
	log.Info("开始进行反向代理...")

	graceful.Shutdown(func() {
		c.Close()
	})
}
