package lock

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lvdashuaibi/littlevote/config"
)

type RedLock struct {
	clients     []*redis.Client
	ctx         context.Context
	locks       map[string]string // key是锁名，value是token值
	timeout     time.Duration
	retries     int
	clusterSize int
}

// NewRedLock 创建新的分布式锁客户端
func NewRedLock() (*RedLock, error) {
	ctx := context.Background()

	// 创建多个独立的Redis客户端
	var clients []*redis.Client

	for _, addr := range config.AppConfig.Redis.LockAddresses {
		client := redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     config.AppConfig.Redis.Password,
			DB:           config.AppConfig.Redis.DB,
			PoolSize:     config.AppConfig.Redis.PoolSize,
			MaxRetries:   config.AppConfig.Redis.MaxRetries,
			DialTimeout:  config.AppConfig.Redis.Timeout,
			ReadTimeout:  config.AppConfig.Redis.Timeout,
			WriteTimeout: config.AppConfig.Redis.Timeout,
		})

		// 测试连接
		if err := client.Ping(ctx).Err(); err != nil {
			log.Printf("Redis锁节点 %s 连接测试失败: %v", addr, err)
			// 关闭已创建的客户端
			for _, c := range clients {
				c.Close()
			}
			return nil, fmt.Errorf("Redis锁节点 %s 连接测试失败: %w", addr, err)
		}

		clients = append(clients, client)
	}

	return &RedLock{
		clients:     clients,
		ctx:         ctx,
		locks:       make(map[string]string),
		timeout:     config.AppConfig.Ticket.LockTimeout,
		retries:     config.AppConfig.Ticket.LockRetryCount,
		clusterSize: len(config.AppConfig.Redis.LockAddresses),
	}, nil
}

// AcquireLock 获取分布式锁
func (r *RedLock) AcquireLock(lockName string, timeout time.Duration) (bool, error) {
	// 生成随机令牌值
	token := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
	success := 0

	// Redlock算法: 尝试在多个节点上获取锁
	for i := 0; i < r.retries; i++ {
		success = 0
		start := time.Now()

		// 尝试在所有Redis节点获取锁
		for i, client := range r.clients {
			// 使用SetNX设置锁
			ok, err := client.SetNX(r.ctx, lockName, token, timeout).Result()
			if err != nil {
				log.Printf("在节点 %s 获取锁 %s 失败: %v", config.AppConfig.Redis.LockAddresses[i], lockName, err)
				continue
			}

			if ok {
				success++
			}
		}

		// 判断是否在多数节点获取成功
		elapsed := time.Since(start)
		validityTime := timeout - elapsed

		if success >= (r.clusterSize/2+1) && validityTime > 0 {
			// 保存锁信息
			r.locks[lockName] = token
			log.Printf("获取锁 %s 成功，Token: %s", lockName, token)
			return true, nil
		}

		// 获取失败，释放所有节点上的锁
		r.unlockAll(lockName, token)

		// 重试前等待一段时间
		time.Sleep(time.Millisecond * 100)
	}

	return false, nil
}

// RefreshLock 刷新锁的过期时间
func (r *RedLock) RefreshLock(lockName string, timeout time.Duration) (bool, error) {
	token, exists := r.locks[lockName]
	if !exists {
		return false, fmt.Errorf("锁 %s 不存在或未持有", lockName)
	}

	// 使用Lua脚本刷新锁，确保只刷新自己持有的锁
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	success := 0
	for i, client := range r.clients {
		result, err := client.Eval(r.ctx, script, []string{lockName}, token, int(timeout/time.Millisecond)).Result()
		if err != nil {
			log.Printf("在节点 %s 刷新锁 %s 失败: %v", config.AppConfig.Redis.LockAddresses[i], lockName, err)
			continue
		}

		if result.(int64) == 1 {
			success++
		}
	}

	if success >= (r.clusterSize/2 + 1) {
		log.Printf("刷新锁 %s 成功", lockName)
		return true, nil
	}

	delete(r.locks, lockName)
	return false, nil
}

// ReleaseLock 释放分布式锁
func (r *RedLock) ReleaseLock(lockName string) error {
	token, exists := r.locks[lockName]
	if !exists {
		return fmt.Errorf("锁 %s 不存在或未持有", lockName)
	}

	r.unlockAll(lockName, token)
	delete(r.locks, lockName)
	log.Printf("释放锁 %s 成功", lockName)
	return nil
}

// unlockAll 在所有节点上释放锁
func (r *RedLock) unlockAll(lockName string, token string) {
	// 使用Lua脚本释放锁，确保只释放自己持有的锁
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	for i, client := range r.clients {
		_, err := client.Eval(r.ctx, script, []string{lockName}, token).Result()
		if err != nil {
			log.Printf("在节点 %s 释放锁 %s 失败: %v", config.AppConfig.Redis.LockAddresses[i], lockName, err)
		}
	}
}

// ReleaseAllLocks 释放所有持有的锁
func (r *RedLock) ReleaseAllLocks() {
	for name, token := range r.locks {
		r.unlockAll(name, token)
		log.Printf("释放锁 %s 成功", name)
	}

	r.locks = make(map[string]string)
}

// Close 关闭分布式锁客户端
func (r *RedLock) Close() error {
	r.ReleaseAllLocks()

	// 关闭所有Redis客户端
	for _, client := range r.clients {
		if err := client.Close(); err != nil {
			log.Printf("关闭Redis客户端失败: %v", err)
		}
	}

	return nil
}
