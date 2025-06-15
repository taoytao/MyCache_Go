package store

import (
	"container/list"
	"sync"
	"time"
)

// lruCache 是基于标准库 list 的 LRU 缓存实现
type lruCache struct {
	mu              sync.RWMutex
	list            *list.List                    //双向链表
	hash            map[string]*list.Element      // 键到链表元素的映射
	expires         map[string]time.Time          // 键到过期时间的映射
	maxBytes        int64                         // 最大缓存字节数
	usedBytes       int64                         // 当前已使用的字节数
	onEvicted       func(key string, value Value) // 驱逐回调函数
	cleanupInterval time.Duration                 // 清理间隔
	cleanupTicker   *time.Ticker                  // 清理定时器
	closech         chan struct{}                 // 关闭信号通道
}

// lruEntry 表示缓存中的一个条目
type lruEntry struct {
	key   string
	value Value
}

// newLRUCache 创建一个新的 LRU 缓存实例
func newLRUCache(opts Options) *lruCache {
	//设置默认清理间隔
	cleanupInterval := opts.CleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}

	LRUCache := &lruCache{
		list:            list.New(),
		hash:            make(map[string]*list.Element),
		expires:         make(map[string]time.Time),
		maxBytes:        opts.MaxBytes,
		onEvicted:       opts.OnEvicted,
		cleanupInterval: cleanupInterval,
		closech:         make(chan struct{}),
	}

	// 启动清理协程
	LRUCache.cleanupTicker = time.NewTicker(LRUCache.cleanupInterval)
	go LRUCache.cleanupLoop()

	return LRUCache
}

// evict 清理过期和超出内存限制的缓存, 调用前必须得持有锁
func (cache *lruCache) evict() {
	// 清理过期项
	now := time.Now()
	for key, expiration := range cache.expires {
		// 过期
		if now.After(expiration) {
			if elem, found := cache.hash[key]; found {
				cache.removeElement(elem)
			}
		}
	}

	// 清理超出内存限制的项
	for cache.usedBytes > cache.maxBytes && cache.list.Len() > 0 {
		elem := cache.list.Front() // 获取最久未使用的项
		if elem != nil {
			cache.removeElement(elem)
		}
	}
}

// cleanupLoop 定期清理过期的缓存条目
func (cache *lruCache) cleanupLoop() {
	for {
		select {
		case <-cache.cleanupTicker.C:
			cache.mu.Lock()
			cache.evict()
			cache.mu.Unlock()
		case <-cache.closech:
			return
		}
	}
}

// removeElement 从缓存中删除指定的元素
func (cache *lruCache) removeElement(elem *list.Element) {
	if elem == nil {
		return
	}

	entry := elem.Value.(*lruEntry)

	// 获取键和值
	key := entry.key
	value := entry.value

	// 从链表中移除元素
	cache.list.Remove(elem)

	// 从哈希表中删除键
	delete(cache.hash, key)

	// 更新已使用字节数
	cache.usedBytes -= int64(value.Len())

	// 如果有驱逐回调函数，调用它
	if cache.onEvicted != nil {
		cache.onEvicted(key, value)
	}

	// 删除过期时间
	delete(cache.expires, key)
}

// Get 从缓存中获取值, 存在且未过期则返回值和true, 否则返回nil和false
func (cache *lruCache) Get(key string) (Value, bool) {
	cache.mu.RLock()

	// 检查key是否存在hash中, 不存在直接返回
	elem, ok := cache.hash[key]
	if !ok {
		cache.mu.RUnlock()
		return nil, false
	}

	// 检查是否过期
	if expiration, exists := cache.expires[key]; exists && time.Now().After(expiration) {
		cache.mu.RUnlock()
		cache.removeElement(elem) // 如果过期了，移除元素
		return nil, false
	}

	// 获取值并释放读锁
	value := elem.Value.(*lruEntry).value
	cache.mu.RUnlock()

	// 更新LRU位置, 上写锁
	cache.mu.Lock()

	// 再次检查元素是否存在(可能在获取读锁后被删除)
	if _, exists := cache.hash[key]; exists {
		// 将元素移动到链表的末尾
		cache.list.MoveToBack(elem)

	}

	cache.mu.Unlock()
	return value, true
}

// SetWithExpiration 设置携带过期时间的缓存值
func (cache *lruCache) SetWithExpiration(key string, value Value, expiration time.Duration) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// 计算过期时间
	var expTime time.Time
	if expiration > 0 {
		expTime = time.Now().Add(expiration)
		cache.expires[key] = expTime // 设置过期时间
	} else {
		delete(cache.expires, key) // 如果没有过期时间，则删除过期时间
	}

	// 如果 key 已经存在, 更新值
	if elem, exists := cache.hash[key]; exists {
		// 更新值
		entry := elem.Value.(*lruEntry)
		cache.usedBytes -= int64(entry.value.Len()) // 减去旧值的字节数
		entry.value = value                         // 更新值
		cache.usedBytes += int64(value.Len())       // 增加新值的字节数

		// 将元素移动到链表的末尾
		cache.list.MoveToBack(elem)
		return nil
	}

	// 如果 key 不存在, 创建新条目
	entry := &lruEntry{key: key, value: value}
	elem := cache.list.PushBack(entry)    // 将新条目添加到链表末尾
	cache.hash[key] = elem                // 更新哈希表
	cache.usedBytes += int64(value.Len()) // 增加新值的字节数

	// 检查是否淘汰旧项
	cache.evict()

	return nil
}

// Set 设置缓存值, 不带过期时间
func (cache *lruCache) Set(key string, value Value) error {
	return cache.SetWithExpiration(key, value, 0)
}

// Delete 从缓存中删除指定键的项
func (cache *lruCache) Delete(key string) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	elem, exists := cache.hash[key]
	if !exists {
		return false // 键不存在
	}

	cache.removeElement(elem) // 移除元素
	return true
}

// Clear 清空缓存
func (cache *lruCache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// 清空链表和哈希表
	cache.list.Init()
	cache.hash = make(map[string]*list.Element)
	cache.expires = make(map[string]time.Time)
	cache.usedBytes = 0

	// 如果设置回调函数，调用它
	if cache.onEvicted != nil {
		for key, elem := range cache.hash {
			value := elem.Value.(*lruEntry).value
			cache.onEvicted(key, value)
		}
	}
}

// Len 返回缓存中的项数
func (c *lruCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.list.Len()
}

// Close 关闭缓存并释放资源
func (cache *lruCache) Close() {
	if cache.cleanupTicker != nil {
		cache.cleanupTicker.Stop() // 停止清理定时器
		close(cache.closech)       // 发送关闭信号
	}

}
