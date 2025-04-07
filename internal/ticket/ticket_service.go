package ticket

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/lock"
	"github.com/lvdashuaibi/littlevote/internal/model"
	"github.com/lvdashuaibi/littlevote/internal/repository"
)

const (
	TicketProducerLockName = "ticket:producer:lock"
)

type TicketService struct {
	redisRepo      *repository.RedisRepository
	mysqlRepo      *repository.MySQLRepository
	redlock        lock.Lock
	refreshTicker  *time.Ticker
	stopChan       chan struct{}
	maxUsageCount  int
	isProducer     bool          // 标识该实例是否为票据生产者
	producerLockCh chan struct{} // 用于同步获取生产者锁的通道
}

func NewTicketService(
	redisRepo *repository.RedisRepository,
	mysqlRepo *repository.MySQLRepository,
	distributedLock lock.Lock,
	isProducer bool,
) *TicketService {
	return &TicketService{
		redisRepo:      redisRepo,
		mysqlRepo:      mysqlRepo,
		redlock:        distributedLock,
		stopChan:       make(chan struct{}),
		maxUsageCount:  config.AppConfig.Ticket.MaxUsageCount,
		isProducer:     isProducer,
		producerLockCh: make(chan struct{}, 1),
	}
}

// StartTicketProducer 启动票据生成器
func (s *TicketService) StartTicketProducer() {
	refreshInterval := config.AppConfig.Ticket.RefreshInterval

	// 如果不是生产者，仍然启动定时器但不会真正生成票据
	s.refreshTicker = time.NewTicker(refreshInterval)

	go func() {

		for {
			select {
			case <-s.refreshTicker.C:
				// 只有被指定为生产者的实例才尝试竞争锁并生成票据
				if s.isProducer {
					s.refreshTicket()
				}
			case <-s.stopChan:
				s.refreshTicker.Stop()
				log.Println("票据生成器已停止")
				return
			}
		}
	}()

	// 启动另一个协程检查生产者状态
	if s.isProducer {
		go s.maintainProducerLock()
	}

	//log.Printf("票据生成器已启动，刷新间隔: %v, 生产者模式: %v", refreshInterval, s.isProducer)
}

// maintainProducerLock 维持生产者锁状态
func (s *TicketService) maintainProducerLock() {
	// 每隔一半的刷新间隔检查一次生产者状态
	checkInterval := config.AppConfig.Ticket.RefreshInterval / 2
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// 初始化时尝试获取生产者锁
	s.tryAcquireProducerLock()

	for {
		select {
		case <-ticker.C:
			s.tryAcquireProducerLock()
		case <-s.stopChan:
			return
		}
	}
}

// tryAcquireProducerLock 尝试获取生产者锁
func (s *TicketService) tryAcquireProducerLock() {
	// 检查生产者锁是否仍然持有
	acquired, err := s.redlock.AcquireLock(TicketProducerLockName, config.AppConfig.Ticket.LockTimeout)
	if err != nil {
		log.Printf("检查票据生成器锁失败: %v", err)
		return
	}

	// 如果成功获取锁，说明之前的锁已经过期或释放
	if acquired {
		//log.Println("重新获取票据生成器锁成功")
		// 继续保持生产者模式
		s.isProducer = true

		// 通知刷新票据的协程
		select {
		case s.producerLockCh <- struct{}{}:
		default:
		}
	}
}

// StopTicketProducer 停止票据生成器
func (s *TicketService) StopTicketProducer() {
	close(s.stopChan)
	// 释放生产者锁
	if s.isProducer {
		s.redlock.ReleaseLock(TicketProducerLockName)
	}
}

// refreshTicket 刷新票据
func (s *TicketService) refreshTicket() {
	var lockAcquired bool
	var err error

	// 检查producerLockCh是否有信号
	select {
	case <-s.producerLockCh:
		// 已在maintainProducerLock中获取了锁
		lockAcquired = true
	default:
		// 尝试获取分布式锁，锁定整个刷新过程
		lockAcquired, err = s.redlock.AcquireLock(TicketProducerLockName, config.AppConfig.Ticket.LockTimeout)
		if err != nil {
			log.Printf("获取票据生成器锁失败: %v", err)
			return
		}
	}

	if !lockAcquired {
		log.Println("未能获取票据生成器锁，跳过当前刷新")
		return
	}

	// 先执行票据生成逻辑
	s.generateTicket()

	// 函数结束时释放锁
	if err := s.redlock.ReleaseLock(TicketProducerLockName); err != nil {
		log.Printf("释放票据生成器锁失败: %v", err)
	}
}

// generateTicket 生成新票据，不包含锁逻辑
func (s *TicketService) generateTicket() {
	// 生成新票据
	version := s.generateVersion()
	ticketValue := s.generateTicketValue()
	now := time.Now()
	expiresAt := now.Add(config.AppConfig.Ticket.RefreshInterval)

	// 创建票据
	ticket := &model.Ticket{
		Value:           ticketValue,
		Version:         version,
		RemainingUsages: s.maxUsageCount,
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
	}

	// 首先保存票据到MySQL（作为主数据源）
	if err := s.mysqlRepo.SaveTicket(ticket); err != nil {
		log.Printf("保存票据到MySQL失败: %v", err)
		return // 如果MySQL保存失败，不继续执行
	}

	// MySQL保存成功后，同步到Redis（作为缓存）
	if err := s.redisRepo.CreateTicket(ticket); err != nil {
		log.Printf("保存票据到Redis失败: %v", err)
		// Redis保存失败不影响整体流程，但记录日志
	}

	// 更新Redis中的最新票据版本
	if err := s.redisRepo.SetNewestTicketVersion(version); err != nil {
		log.Printf("设置Redis最新票据版本失败: %v", err)
		// Redis更新失败不影响整体流程，但记录日志
	}

	//log.Printf("已生成新票据: 版本=%s, 过期时间=%v", version, expiresAt)
}

// GetCurrentTicket 获取当前票据
func (s *TicketService) GetCurrentTicket(clientID string) (*model.Ticket, error) {
	// 优先从Redis获取最新票据版本
	version, err := s.redisRepo.GetNewestTicketVersion()
	// if err != nil || version == "" {
	// 	// Redis获取失败或无版本，尝试从MySQL获取
	// 	log.Printf("从Redis获取最新票据版本失败: %v，尝试从MySQL获取", err)
	// 	mysqlVersion, mysqlErr := s.mysqlRepo.GetNewestTicketVersion()
	// 	if mysqlErr != nil {
	// 		return nil, fmt.Errorf("获取最新票据版本失败: %w", mysqlErr)
	// 	}

	// 	if mysqlVersion == "" {
	// 		return nil, fmt.Errorf("票据尚未生成")
	// 	}

	// 	// 更新Redis中的最新版本
	// 	if mysqlVersion != "" {
	// 		if setErr := s.redisRepo.SetNewestTicketVersion(mysqlVersion); setErr != nil {
	// 			log.Printf("更新Redis最新票据版本失败: %v", setErr)
	// 		}
	// 	}

	// 	version = mysqlVersion
	// }

	// 从Redis获取票据
	redisTicket, err := s.redisRepo.GetTicket(version)
	if err != nil {
		// Redis查询失败时，尝试从MySQL获取
		log.Printf("从Redis获取票据失败: %v，尝试从MySQL获取", err)

		mysqlTicket, mysqlErr := s.mysqlRepo.GetTicket(version)
		if mysqlErr != nil {
			// MySQL也失败，返回错误
			return nil, fmt.Errorf("获取票据失败: %w", mysqlErr)
		}

		// MySQL查询成功，将数据写回Redis
		if err := s.redisRepo.CreateTicket(mysqlTicket); err != nil {
			log.Printf("将MySQL票据同步到Redis失败: %v", err)
		}

		// 检查剩余使用次数
		if mysqlTicket.RemainingUsages <= 0 {
			return nil, fmt.Errorf("票据 %s 使用次数已耗尽", version)
		}

		//log.Printf("客户端 %s 已获取票据(MySQL): 版本=%s", clientID, version)
		return mysqlTicket, nil
	}

	// Redis查询成功，检查剩余使用次数
	if redisTicket.RemainingUsages <= 0 {
		return nil, fmt.Errorf("票据 %s 使用次数已耗尽", version)
	}

	//log.Printf("客户端 %s 已获取票据(Redis): 版本=%s", clientID, version)
	return redisTicket, nil
}

// ValidateTicket 验证票据
func (s *TicketService) ValidateTicket(ticket *model.Ticket) (bool, error) {
	return s.redisRepo.ValidateTicket(ticket)
}

// UseTicket 使用票据
func (s *TicketService) UseTicket(ticket *model.Ticket) (bool, error) {
	// 验证票据
	valid, err := s.ValidateTicket(ticket)
	if err != nil {
		return false, fmt.Errorf("票据验证失败: %w", err)
	}

	if !valid {
		return false, fmt.Errorf("票据无效")
	}

	// 尝试减少Redis中的票据使用次数
	redisRemaining, err := s.redisRepo.DecrementTicketUsage(ticket.Version)
	if err != nil {
		return false, fmt.Errorf("减少Redis票据使用次数失败: %w", err)
	}
	redisRemaining++

	//log.Printf("票据 %s 使用成功，剩余使用次数: %d", ticket.Version, redisRemaining)
	return true, nil
}

// generateVersion 生成票据版本号
func (s *TicketService) generateVersion() string {
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%d", timestamp)
}

// generateTicketValue 生成票据值
func (s *TicketService) generateTicketValue() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("生成随机票据值失败: %v", err)
		// 使用时间戳作为备选
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
