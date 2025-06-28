package myCache

import (
	"context"
	"fmt"
	"time"

	pb "MyCache_Go/pb"

	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr    string
	svcName string
	etcdCli *clientv3.Client
	conn    *grpc.ClientConn
	grpcCli pb.MyCacheClient
}

var _ Peer = (*Client)(nil)

// // NewClient 创建一个新的缓存客户端
func NewClient(addr, svcName string, etcdCli *clientv3.Client) (*Client, error) {
	var err error
	if etcdCli == nil {
		etcdCli, err = clientv3.New(clientv3.Config{
			Endpoints:   []string{"localhost:2379"},
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create etcd client: %v", err)
		}
	}

	conn, err := grpc.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(10*time.Second),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial server: %v", err)
	}

	grpcClient := pb.NewMyCacheClient(conn)

	client := &Client{
		addr:    addr,
		svcName: svcName,
		etcdCli: etcdCli,
		conn:    conn,
		grpcCli: grpcClient,
	}

	return client, nil
}

// Get 从缓存中获取值
func (c *Client) Get(group, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := c.grpcCli.Get(ctx, &pb.Request{
		Group: group,
		Key:   key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get value from kamacache: %v", err)
	}

	return resp.GetValue(), nil
}

// Set 向缓存中设置值
func (c *Client) Set(ctx context.Context, group, key string, value []byte) error {
	resp, err := c.grpcCli.Set(ctx, &pb.Request{
		Group: group,
		Key:   key,
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("failed to set value to kamacache: %v", err)
	}
	logrus.Infof("grpc set request resp: %+v", resp)

	return nil
}

// Delete 从缓存中删除值
func (c *Client) Delete(group, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := c.grpcCli.Delete(ctx, &pb.Request{
		Group: group,
		Key:   key,
	})
	if err != nil {
		return false, fmt.Errorf("failed to delete value from kamacache: %v", err)
	}

	return resp.GetValue(), nil
}

// Close 关闭客户端连接
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
