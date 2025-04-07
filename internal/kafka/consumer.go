package kafka

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/model"
	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	readers    []*kafka.Reader
	ctx        context.Context
	cancel     context.CancelFunc
	numWorkers int
	wg         sync.WaitGroup
}

type MessageHandler func(event *model.VoteEvent) error

func NewConsumer() (*Consumer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	numWorkers := 8 // 使用8个goroutine并发消费

	// 获取Kafka主题的分区数量
	conn, err := kafka.DialLeader(ctx, "tcp", config.AppConfig.Kafka.Brokers[0], config.AppConfig.Kafka.Topic, 0)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions()
	if err != nil {
		return nil, err
	}

	// 统计主题的分区数量
	var topicPartitions []int
	for _, p := range partitions {
		if p.Topic == config.AppConfig.Kafka.Topic {
			topicPartitions = append(topicPartitions, p.ID)
		}
	}

	log.Printf("检测到Kafka主题 %s 有 %d 个分区", config.AppConfig.Kafka.Topic, len(topicPartitions))

	// 创建多个reader，每个reader负责一个或多个分区
	readers := make([]*kafka.Reader, 0, numWorkers)

	// 如果分区数量小于worker数量，需要调整并发消费的worker数量
	actualWorkers := min(numWorkers, len(topicPartitions))
	if actualWorkers < numWorkers {
		log.Printf("分区数量(%d)小于期望的goroutine数量(%d), 将使用%d个goroutine消费",
			len(topicPartitions), numWorkers, actualWorkers)
		numWorkers = actualWorkers
	}

	// 方案1: 每个工作线程处理自己的特定分区
	if len(topicPartitions) > 0 {
		for i := 0; i < numWorkers; i++ {
			// 为每个工作线程确定要处理的分区
			partitionIndex := i % len(topicPartitions)
			partition := topicPartitions[partitionIndex]

			// 为每个分区创建一个独立的reader
			reader := kafka.NewReader(kafka.ReaderConfig{
				Brokers:   config.AppConfig.Kafka.Brokers,
				Topic:     config.AppConfig.Kafka.Topic,
				Partition: partition,
				MinBytes:  10e3, // 10KB
				MaxBytes:  10e6, // 10MB
			})

			readers = append(readers, reader)
			log.Printf("消费者工作线程 #%d 将处理分区: %d", i, partition)
		}
	}

	// 方案2(备选): 使用消费者组模式，但会失去对分区的精确控制
	// 如果分区数为0或者分区Reader创建失败，使用消费者组模式
	if len(readers) == 0 {
		log.Printf("未检测到分区或分区Reader创建失败，将使用消费者组模式")
		groupReader := kafka.NewReader(kafka.ReaderConfig{
			Brokers:  config.AppConfig.Kafka.Brokers,
			Topic:    config.AppConfig.Kafka.Topic,
			GroupID:  config.AppConfig.Kafka.GroupID,
			MinBytes: 10e3, // 10KB
			MaxBytes: 10e6, // 10MB
		})
		readers = append(readers, groupReader)
		log.Printf("创建消费者组Reader，GroupID: %s", config.AppConfig.Kafka.GroupID)
		numWorkers = 1 // 消费者组模式只使用一个Reader
	}

	return &Consumer{
		readers:    readers,
		ctx:        ctx,
		cancel:     cancel,
		numWorkers: numWorkers,
	}, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StartConsuming 开始消费消息，使用多个goroutine并发消费
func (c *Consumer) StartConsuming(handler MessageHandler) {
	for i := 0; i < len(c.readers); i++ {
		reader := c.readers[i]
		if reader == nil {
			continue
		}

		c.wg.Add(1)
		go func(workerID int, r *kafka.Reader) {
			defer c.wg.Done()
			c.consumeMessages(workerID, r, handler)
		}(i, reader)
	}

	log.Printf("已启动 %d 个Kafka消费者工作线程", len(c.readers))
}

// consumeMessages 单个消费者goroutine的消费逻辑
func (c *Consumer) consumeMessages(workerID int, reader *kafka.Reader, handler MessageHandler) {
	log.Printf("消费者工作线程 #%d 已启动", workerID)

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("消费者工作线程 #%d 收到停止信号", workerID)
			return
		default:
			m, err := reader.ReadMessage(c.ctx)
			if err != nil {
				if err == context.Canceled {
					log.Printf("消费者工作线程 #%d 上下文已取消", workerID)
					return
				}
				log.Printf("消费者工作线程 #%d 读取消息失败: %v", workerID, err)
				time.Sleep(time.Second)
				continue
			}

			var event model.VoteEvent
			if err := json.Unmarshal(m.Value, &event); err != nil {
				log.Printf("消费者工作线程 #%d 解析消息失败: %v", workerID, err)
				continue
			}

			//log.Printf("消费者工作线程 #%d 收到消息: 分区=%d, 偏移量=%d, 版本=%s",
			//workerID, m.Partition, m.Offset, event.TicketVersion)

			if err := handler(&event); err != nil {
				//log.Printf("消费者工作线程 #%d 处理消息失败: %v", workerID, err)
			}
		}
	}
}

// Stop 停止消费
func (c *Consumer) Stop() error {
	log.Println("正在停止所有Kafka消费者工作线程...")
	c.cancel()

	// 等待所有工作线程结束
	c.wg.Wait()

	// 关闭所有reader
	for i, reader := range c.readers {
		if reader != nil {
			if err := reader.Close(); err != nil {
				log.Printf("关闭消费者 #%d 失败: %v", i, err)
			}
		}
	}

	log.Println("所有Kafka消费者工作线程已停止")
	return nil
}
