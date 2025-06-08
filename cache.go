package myCache

import (
	"MyCache_Go/store"
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// CacheOptions 缓存配置选项
type CacheOptions struct {
	// CacheType    store.CacheType                     // 缓存类型: LRU, LRU2 等
	MaxBytes     int64         // 最大内存使用量
	BucketCount  uint16        // 缓存桶数量 (用于 LRU2)
	CapPerBucket uint16        // 每个缓存桶的容量 (用于 LRU2)
	Level2Cap    uint16        // 二级缓存桶的容量 (用于 LRU2)
	CleanupTime  time.Duration // 清理间隔
	// OnEvicted    func(key string, value store.Value) // 驱逐回调
}

// Cache 是对底层缓存存储的封装
type Cache struct {
	mu          sync.RWMutex // 读写锁
	store       store.Store  // 底层存储实现
	opts        CacheOptions // 缓存配置选项
	hits        int64        // 缓存命中次数
	misses      int64        // 缓存未命中次数
	initialized int32        // 原子变量，标记缓存是否已初始化
	closed      int32        // 原子变量，标记缓存是否已关闭
}

// NewCache 创建一个新的缓存实例
func NewCache(opts CacheOptions) *Cache {
	return &Cache{
		opts: opts,
	}
}

// Get 从缓存中获取值
func (cache *Cache) Get(ctx context.Context, key string) (value ByteView, ok bool) {
	if atomic.LoadInt32(&cache.closed) == 1 {
		return ByteView{}, false
	}

	// 如果缓存未初始化，直接返回未命中
	if atomic.LoadInt32(&cache.initialized) == 0 {
		atomic.AddInt64(&cache.misses, 1)
		return ByteView{}, false
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// 从底层存储获取值
	val, found := cache.store.Get(key)
	if !found {
		// 如果未找到，增加未命中计数
		atomic.AddInt64(&cache.misses, 1)
		return ByteView{}, false
	}

	// 如果找到，增加命中计数
	atomic.AddInt64(&cache.hits, 1)

	// 返回只读的字节视图
	if bv, ok := val.(ByteView); ok {
		return bv, true
	}

	// 类型断言失败
	logrus.Warnf("Type assertion failed for key %s, expected ByteView", key)
	atomic.AddInt64(&cache.misses, 1)
	return ByteView{}, false
}
