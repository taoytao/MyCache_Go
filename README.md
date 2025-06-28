# MyCache_Go

## 核心特性
### 1. 分布式架构
* 基于 etcd 的服务注册与发现
* 一致性哈希实现负载均衡
* 节点自动发现和同步
* 支持动态扩缩容
  
### 2. 缓存功能
* 支持LRU/LRU2缓存策略
* 可配置过期时间
* singleflight防止缓存击穿

### 3. 高性能设计
* 并发安全
* 异步数据同步
* 防缓存击穿
* gRPC通信

## 快速开始
### 1. 安装
`go get github.com/taoytao/MyCache_Go`

### 2. 启动etcd
**使用 Docker 启动 etcd**  

`docker run -d --name etcd -p 2379:2379 quay.io/coreos/etcd:v3.5.0 etcd --advertise-client-urls http://0.0.0.0:2379 --listen-client-urls http://0.0.0.0:2379`

### 3. 测试demo
参考代码路径test/test.go

### 4. 多节点部署
`# 启动节点 A
go run example/test.go -port 8001 -node A`  

`# 启动节点 B
go run example/test.go -port 8002 -node B`  

`# 启动节点 C
go run example/test.go -port 8003 -node C`  

### 5. 查看测试结果
