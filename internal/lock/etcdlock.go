package lock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lvdashuaibi/littlevote/config"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	defaultTTL = 10 // 默认锁过期时间（秒）
)

// EtcdLock 实现分布式锁接口
type EtcdLock struct {
	client *clientv3.Client
	mu     sync.Mutex            // 保护locks的互斥锁
	locks  map[string]*lockEntry // 当前持有的锁
}

type lockEntry struct {
	leaseID clientv3.LeaseID
	key     string
	cancel  context.CancelFunc // 用于停止自动续约
}

func NewETCDLock() (*EtcdLock, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   config.AppConfig.ETCD.Endpoints,
		DialTimeout: config.AppConfig.ETCD.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("创建etcd客户端失败: %v", err)
	}

	return &EtcdLock{
		client: cli,
		locks:  make(map[string]*lockEntry),
	}, nil
}

func (el *EtcdLock) AcquireLock(lockName string, timeout time.Duration) (bool, error) {
	el.mu.Lock()
	defer el.mu.Unlock()

	// 检查是否已持有锁
	if _, ok := el.locks[lockName]; ok {
		return false, fmt.Errorf("锁 %s 已被当前实例持有", lockName)
	}

	key := fmt.Sprintf("/locks/%s", lockName)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// 创建租约
	lease := clientv3.NewLease(el.client)
	grantResp, err := lease.Grant(ctx, defaultTTL)
	if err != nil {
		cancel()
		return false, fmt.Errorf("创建租约失败: %v", err)
	}

	// 尝试获取锁
	txn := el.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, "", clientv3.WithLease(grantResp.ID))).
		Else()

	txnResp, err := txn.Commit()
	if err != nil {
		cancel()
		lease.Revoke(context.Background(), grantResp.ID)
		return false, fmt.Errorf("事务执行失败: %v", err)
	}

	if !txnResp.Succeeded {
		cancel()
		lease.Revoke(context.Background(), grantResp.ID)
		return false, nil
	}

	// 启动自动续约
	keepAliveCtx, keepAliveCancel := context.WithCancel(context.Background())
	go el.keepAlive(keepAliveCtx, grantResp.ID)

	// 记录锁信息
	el.locks[lockName] = &lockEntry{
		leaseID: grantResp.ID,
		key:     key,
		cancel:  keepAliveCancel,
	}

	return true, nil
}

func (el *EtcdLock) RefreshLock(lockName string, timeout time.Duration) (bool, error) {
	el.mu.Lock()
	defer el.mu.Unlock()

	entry, ok := el.locks[lockName]
	if !ok {
		return false, fmt.Errorf("未持有锁 %s", lockName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 续租约
	_, err := clientv3.NewLease(el.client).KeepAliveOnce(ctx, entry.leaseID)
	if err != nil {
		if err == rpctypes.ErrLeaseNotFound {
			delete(el.locks, lockName)
			return false, nil
		}
		return false, fmt.Errorf("续约失败: %v", err)
	}

	return true, nil
}

func (el *EtcdLock) ReleaseLock(lockName string) error {
	el.mu.Lock()
	defer el.mu.Unlock()

	return el.releaseLock(lockName)
}

func (el *EtcdLock) ReleaseAllLocks() {
	el.mu.Lock()
	defer el.mu.Unlock()

	for lockName := range el.locks {
		el.releaseLock(lockName)
	}
}

func (el *EtcdLock) Close() error {
	el.ReleaseAllLocks()
	return el.client.Close()
}

// 内部自动续约方法
func (el *EtcdLock) keepAlive(ctx context.Context, leaseID clientv3.LeaseID) {
	lease := clientv3.NewLease(el.client)
	ticker := time.NewTicker(time.Duration(defaultTTL/2) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_, err := lease.KeepAliveOnce(ctx, leaseID)
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// 内部释放锁方法
func (el *EtcdLock) releaseLock(lockName string) error {
	entry, ok := el.locks[lockName]
	if !ok {
		return nil
	}

	// 停止自动续约
	entry.cancel()

	// 删除键
	_, err := el.client.Delete(context.Background(), entry.key)
	if err != nil {
		return fmt.Errorf("删除键失败: %v", err)
	}

	// 释放租约
	_, err = clientv3.NewLease(el.client).Revoke(context.Background(), entry.leaseID)
	if err != nil {
		return fmt.Errorf("释放租约失败: %v", err)
	}

	delete(el.locks, lockName)
	return nil
}
