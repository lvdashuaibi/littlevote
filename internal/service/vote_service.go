package service

import (
	"fmt"
	"log"
	"time"

	"github.com/lvdashuaibi/littlevote/internal/kafka"
	"github.com/lvdashuaibi/littlevote/internal/model"
	"github.com/lvdashuaibi/littlevote/internal/repository"
	"github.com/lvdashuaibi/littlevote/internal/ticket"
)

type VoteService struct {
	mysqlRepo     *repository.MySQLRepository
	redisRepo     *repository.RedisRepository
	ticketService *ticket.TicketService
	kafkaProducer *kafka.Producer
}

func NewVoteService(
	mysqlRepo *repository.MySQLRepository,
	redisRepo *repository.RedisRepository,
	ticketService *ticket.TicketService,
	kafkaProducer *kafka.Producer,
) *VoteService {
	return &VoteService{
		mysqlRepo:     mysqlRepo,
		redisRepo:     redisRepo,
		ticketService: ticketService,
		kafkaProducer: kafkaProducer,
	}
}

// GetTicket 获取票据
func (s *VoteService) GetTicket(clientID string) (*model.Ticket, error) {
	return s.ticketService.GetCurrentTicket(clientID)
}

// Vote 投票
func (s *VoteService) Vote(request *model.VoteRequest) (*model.VoteResponse, error) {
	failedResponse := &model.VoteResponse{
		Success:   false,
		Message:   "投票失败",
		Usernames: request.Usernames,
		Timestamp: time.Now(),
	}

	// 验证用户名列表非空
	if len(request.Usernames) == 0 {
		return failedResponse, fmt.Errorf("用户名列表不能为空")
	}

	// 验证用户名是否符合规范（A-Z）
	for _, username := range request.Usernames {
		if len(username) != 1 || username[0] < 'A' || username[0] > 'Z' {
			return failedResponse, fmt.Errorf("无效的用户名: %s, 用户名必须是A-Z之间的单个字母", username)
		}
	}

	// 使用票据
	used, err := s.ticketService.UseTicket(&request.Ticket)
	if err != nil {
		return failedResponse, fmt.Errorf("使用票据失败: %w", err)
	}
	if !used {
		return failedResponse, fmt.Errorf("票据使用失败")
	}

	// 创建投票事件并发送到Kafka
	voteEvent := &model.VoteEvent{
		Usernames:     request.Usernames,
		TicketVersion: request.Ticket.Version,
		VotedAt:       time.Now(),
	}

	if err := s.kafkaProducer.SendVoteEvent(voteEvent); err != nil {
		log.Printf("发送投票事件到Kafka失败: %v", err)
		// 即使消息发送失败，我们也直接更新数据库，以确保数据一致性
		// 同步更新数据库
		if err := s.mysqlRepo.IncrementVotes(request.Usernames, request.Ticket.Version); err != nil {
			return failedResponse, fmt.Errorf("更新数据库失败: %w", err)
		}

		// 清除用户缓存，确保下次读取时获取最新数据
		for _, username := range request.Usernames {
			if err := s.redisRepo.DeleteUserVoteCache(username); err != nil {
				log.Printf("删除用户 %s 缓存失败: %v", username, err)
			}
		}
	}

	// 返回投票结果
	return &model.VoteResponse{
		Success:   true,
		Message:   "投票成功",
		Usernames: request.Usernames,
		Timestamp: time.Now(),
	}, nil
}

// GetUserVote 获取用户票数
func (s *VoteService) GetUserVote(username string) (*model.UserVote, error) {
	// 验证用户名是否符合规范（A-Z）
	if len(username) != 1 || username[0] < 'A' || username[0] > 'Z' {
		return nil, fmt.Errorf("无效的用户名: %s, 用户名必须是A-Z之间的单个字母", username)
	}

	// 先从缓存获取
	userVote, found, err := s.redisRepo.GetUserVote(username)
	if err != nil {
		//log.Printf("获取用户 %s 缓存失败: %v", username, err)
	}

	if found && userVote != nil {
		return userVote, nil
	}

	// 缓存未命中，从数据库获取
	userVote, err = s.mysqlRepo.GetUserVote(username)
	if err != nil {
		return nil, fmt.Errorf("获取用户 %s 票数失败: %w", username, err)
	}

	// 更新缓存
	if err := s.redisRepo.SetUserVote(userVote); err != nil {
		//log.Printf("更新用户 %s 缓存失败: %v", username, err)
	}

	return userVote, nil
}

// GetAllUserVotes 获取所有用户票数
func (s *VoteService) GetAllUserVotes() ([]*model.UserVote, error) {
	return s.mysqlRepo.GetAllUserVotes()
}

// ProcessVoteEvent 处理投票事件（消费者使用）
func (s *VoteService) ProcessVoteEvent(event *model.VoteEvent) error {
	// 更新数据库
	if err := s.mysqlRepo.IncrementVotes(event.Usernames, event.TicketVersion); err != nil {
		return fmt.Errorf("处理投票事件更新数据库失败: %w", err)
	}
	if _, err := s.mysqlRepo.DecrementTicketUsage(event.TicketVersion); err != nil {
		return fmt.Errorf("处理投票事件减少票据使用次数失败: %w", err)
	}

	// 清除用户缓存
	for _, username := range event.Usernames {
		if err := s.redisRepo.DeleteUserVoteCache(username); err != nil {
			log.Printf("处理投票事件删除用户 %s 缓存失败: %v", username, err)
		}
	}

	//log.Printf("处理投票事件成功: 票据版本=%s, 用户=%v", event.TicketVersion, event.Usernames)
	return nil
}

// TicketAndVote 获取票据并立即投票
func (s *VoteService) TicketAndVote(usernames []string) (*model.VoteResponse, error) {
	// 生成客户端ID
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())

	// 步骤1: 获取票据
	ticket, err := s.ticketService.GetCurrentTicket(clientID)
	if err != nil {
		return &model.VoteResponse{
			Success:   false,
			Message:   fmt.Sprintf("获取票据失败: %v", err),
			Usernames: usernames,
			Timestamp: time.Now(),
		}, nil
	}

	// 步骤2: 使用获取到的票据进行投票
	voteRequest := &model.VoteRequest{
		Usernames: usernames,
		Ticket:    *ticket,
	}

	return s.Vote(voteRequest)
}
