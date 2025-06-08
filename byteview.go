package myCache

// ByteView 只读的字节视图，用于缓存数据
type ByteView struct {
	b []byte
}

func (b ByteView) Len() int {
	return len(b.b)
}
