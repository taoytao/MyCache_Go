package myCache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"../singleflight"

	"github.com/sirupsen/logrus"
)

var (
	groupsMu sync.RWMutex
	groups   = make(map[string]*Group)
)

// ErrKeyRequired 键不能为空错误
var ErrKeyRequired = errors.New("key is required")

// ErrValueRequired 值不能为空错误
var ErrValueRequired = errors.New("value is required")

// ErrGroupClosed 组已关闭错误
var ErrGroupClosed = errors.New("cache group is closed")

// Getter: 加载键值的回调函数接口
type Getter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// Group 是一个缓存命名空间
type Group struct {
	name      string
	getter    Getter
	mainCache *Cache
	// peers      PeerPicker
	loader     *singleflight.Group
	expiration time.Duration // 缓存过期时间，0表示永不过期
	closed     int32         // 原子变量，标记组是否已关闭
	stats      groupStats    // 统计信息
}

// groupStats 保存组的统计信息
type groupStats struct {
	loads        int64 // 加载次数
	localHits    int64 // 本地缓存命中次数
	localMisses  int64 // 本地缓存未命中次数
	peerHits     int64 // 从对等节点获取成功次数
	peerMisses   int64 // 从对等节点获取失败次数
	loaderHits   int64 // 从加载器获取成功次数
	loaderErrors int64 // 从加载器获取失败次数
	loadDuration int64 // 加载总耗时（纳秒）
}

// 定义一个缓存组，包含名称、缓存大小、数据获取器和节点信息
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	if getter == nil {
		panic("getter is nil")
	}
	if cacheBytes <= 0 {
		panic("cacheBytes must be greater than 0")
	}

	group := &Group{
		name:   name,
		getter: getter,
		// mainCache: NewCache(cacheOpts),
		// peers: new(sync.Map),
	}

	groupsMu.Lock()
	defer groupsMu.Unlock()

	// 注册到全局组映射
	groups[name] = group

	return group
}

// load 加载数据
func (g *Group) load(ctx context.Context, key string) (value ByteView, err error) {
	// 使用 singleflight 确保并发请求只加载一次
	startTime := time.Now()
	viewi, err := g.loader.Do(key, func() (interface{}, error) {
		return g.loadData(ctx, key)
	})

	// 记录加载时间
	loadDuration := time.Since(startTime).Nanoseconds()
	atomic.AddInt64(&g.stats.loadDuration, loadDuration)
	atomic.AddInt64(&g.stats.loads, 1)

	if err != nil {
		atomic.AddInt64(&g.stats.loaderErrors, 1)
		return ByteView{}, err
	}

	view := viewi.(ByteView)

	// 设置到本地缓存
	if g.expiration > 0 {
		g.mainCache.AddWithExpiration(key, view, time.Now().Add(g.expiration))
	} else {
		g.mainCache.Add(key, view)
	}

	return view, nil
}

// loadData 实际加载数据的方法
func (g *Group) loadData(ctx context.Context, key string) (value ByteView, err error) {
	// 尝试从远程节点获取
	if g.peers != nil {
		peer, ok, isSelf := g.peers.PickPeer(key)
		if ok && !isSelf {
			value, err := g.getFromPeer(ctx, peer, key)
			if err == nil {
				atomic.AddInt64(&g.stats.peerHits, 1)
				return value, nil
			}

			atomic.AddInt64(&g.stats.peerMisses, 1)
			logrus.Warnf("[KamaCache] failed to get from peer: %v", err)
		}
	}

	// 从数据源加载
	bytes, err := g.getter.Get(ctx, key)
	if err != nil {
		return ByteView{}, fmt.Errorf("failed to get data: %w", err)
	}

	atomic.AddInt64(&g.stats.loaderHits, 1)
	return ByteView{b: cloneBytes(bytes)}, nil
}

// Get: 从缓存获取数据
func (group *Group) Get(ctx context.Context, key string) (ByteView, error) {
	// 原子操作: 检查组是否已关闭
	if atomic.LoadInt32(&group.closed) == 1 {
		return ByteView{}, ErrGroupClosed
	}

	if key == "" {
		return ByteView{}, ErrKeyRequired
	}

	// 从本地缓存获取
	view, ok := group.mainCache.Get(ctx, key)
	if ok {
		atomic.AddInt64(&group.stats.localHits, 1)
		return view, nil
	}

	atomic.AddInt64(&group.stats.localMisses, 1)

	// 尝试从其他节点获取或加载
	return group.load(ctx, key)
}
