# Proxy

使用公网IP反向代理本地局域网服务的实现。

之前看到一篇 [如何搭建一台永久运行的个人服务器？](https://mp.weixin.qq.com/s/WF0EQMO-84mUhabG0Slbhg) 的文章，随之就滋生了自己做一个反向代理工具的想法，使自己局域网的服务能映射到公网地址。如果你有以下需求就可以使用：

1. 云服务器受限：存储或计算有限，但带宽满足要求。
2. 远程协同开发：没有VPN的条件下需要进行不同局域网下的协同开发。
3. 乐于助人：为没有云服务器的开发者提供公网访问IP。  

希望感兴趣的开发者和我一起完善该项目。

## Usage

### 代理服务端

* 配置文件

  ```yaml
  # 向外暴露的 HTTP Base URL
  baseURL: http://127.0.0.1:9090
  
  # GRPC 服务地址
  rpcAddress: 0.0.0.0:9091
  
  # HTTP 服务地址
  httpAddress: 0.0.0.0:9090
  
  # 是否启用 TLS
  enableTLS: true
  
  # Bolt 数据库路径
  boltDBPath: my.db
  
  # 日志配置
  logger:
    # 日志输出类型。1 代表输出到控制台; 2 代表输出到文件
    type: 1
  
    # 日志等级。0 DEBUG; 1 INFO; 2 WARN; 3 ERROR; 4 FATAL
    level: 0
  
    # 日志文件输出路径
    path: ./log
  
    # 日志文件大小上限，用于分片
    capacity: 65536 # 64KB
  ```

* 运行

  ```sh
  cd proxy && make server
  ```

### 代理客户端

* 配置文件

  ```yaml
  # GRPC 代理服务地址
  serverAddress: "127.0.0.1:9091"
  
  # 代理本地HTTP服务的 URL
  httpBaseURL: "http://127.0.0.1:18080"
  
  # 代理本地Websocket服务的 URL
  wsBaseURL: "ws://127.0.0.1:18080/ws"
  
  # 是否启用 TLS
  enableTLS: true
  ```

* 运行

  ```shell
  cd proxy/ && make client
  ```

## TODO

- [x] Http反向代理
- [x] Webscoket反向代理
- [ ] 前端静态页面反向代理
- [ ] 随机域名