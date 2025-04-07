package lock

import (
	"time"
)

// Lock 分布式锁接口
type Lock interface {
	// AcquireLock 获取分布式锁
	// 返回值：bool表示是否成功获取锁，error表示获取过程中的错误
	AcquireLock(lockName string, timeout time.Duration) (bool, error)

	// RefreshLock 刷新锁的过期时间
	// 返回值：bool表示是否成功刷新锁，error表示刷新过程中的错误
	RefreshLock(lockName string, timeout time.Duration) (bool, error)

	// ReleaseLock 释放分布式锁
	// 返回值：error表示释放过程中的错误
	ReleaseLock(lockName string) error

	// ReleaseAllLocks 释放所有持有的锁
	ReleaseAllLocks()

	// Close 关闭分布式锁客户端
	// 返回值：error表示关闭过程中的错误
	Close() error
}
