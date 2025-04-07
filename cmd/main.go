package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/api/graph"
	intkafka "github.com/lvdashuaibi/littlevote/internal/kafka"
	"github.com/lvdashuaibi/littlevote/internal/lock"
	"github.com/lvdashuaibi/littlevote/internal/repository"
	"github.com/lvdashuaibi/littlevote/internal/service"
	"github.com/lvdashuaibi/littlevote/internal/ticket"
)

const (
	ServiceStartLockName = "littlevote:service:start:lock"
	LockAcquireTimeout   = 30 * time.Second
)

var (
	configPath = flag.String("config", "config/config.yaml", "配置文件路径")
	instanceID = flag.Int("instance", 1, "实例ID，用于区分多个实例")
)

func main() {
	// 解析命令行参数
	flag.Parse()

	// 加载配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("配置加载成功，当前实例ID: %d", *instanceID)

	// 创建数据库连接
	mysqlRepo, err := repository.NewMySQLRepository()
	if err != nil {
		log.Fatalf("初始化MySQL仓库失败: %v", err)
	}
	defer mysqlRepo.Close()
	log.Printf("MySQL仓库初始化成功")

	// 创建Redis连接
	redisRepo, err := repository.NewRedisRepository()
	if err != nil {
		log.Fatalf("初始化Redis仓库失败: %v", err)
	}
	defer redisRepo.Close()
	log.Printf("Redis仓库初始化成功")

	// 创建分布式锁
	distributedLock, err := lock.NewETCDLock()
	if err != nil {
		log.Fatalf("初始化ETCD分布式锁失败: %v", err)
	}
	defer distributedLock.Close()
	log.Printf("ETCD分布式锁初始化成功")

	// 获取服务启动锁
	lockAcquired, err := distributedLock.AcquireLock(ServiceStartLockName, LockAcquireTimeout)
	if err != nil {
		log.Printf("获取服务启动锁失败: %v，将以非票据生产者模式启动", err)
	}

	var isTicketProducer bool
	if lockAcquired {
		log.Printf("实例 %d 获取服务启动锁成功，将作为票据生产者启动", *instanceID)
		isTicketProducer = true
		defer distributedLock.ReleaseLock(ServiceStartLockName)
	} else {
		log.Printf("实例 %d 未获取到服务启动锁，以普通节点模式启动", *instanceID)
		isTicketProducer = false
	}

	// 创建Kafka生产者
	producer, err := intkafka.NewProducer()
	if err != nil {
		log.Fatalf("初始化Kafka生产者失败: %v", err)
	}
	defer producer.Close()
	log.Printf("Kafka生产者初始化成功")

	// 创建Kafka消费者
	consumer, err := intkafka.NewConsumer()
	if err != nil {
		log.Fatalf("初始化Kafka消费者失败: %v", err)
	}
	defer consumer.Stop()
	log.Printf("Kafka消费者初始化成功")

	// 创建票据服务
	ticketService := ticket.NewTicketService(redisRepo, mysqlRepo, distributedLock, isTicketProducer)

	// 启动票据生产器 (只有获取锁的实例才会真正生成票据)
	ticketService.StartTicketProducer()
	defer ticketService.StopTicketProducer()
	log.Printf("票据服务初始化成功，票据生产者模式: %v", isTicketProducer)

	// 创建投票服务
	voteService := service.NewVoteService(mysqlRepo, redisRepo, ticketService, producer)
	log.Printf("投票服务初始化成功")

	// 启动Kafka消费者
	consumer.StartConsuming(voteService.ProcessVoteEvent)
	log.Printf("Kafka消费者已启动")

	// 创建GraphQL服务
	graphqlServer := graph.NewGraphQLServer(voteService)
	log.Printf("GraphQL服务初始化成功")

	// 计算端口，支持多实例
	serverPort := cfg.Server.Port + *instanceID - 1

	// 启动HTTP服务器(异步)
	go func() {
		if err := graphqlServer.Start(serverPort); err != nil {
			log.Fatalf("启动GraphQL服务器失败: %v", err)
		}
	}()

	log.Printf("Little Vote 系统 (实例 %d) 已启动，服务地址: http://localhost:%d", *instanceID, serverPort)

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭服务...")
}
