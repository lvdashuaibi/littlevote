package model

import (
	"time"
)

// UserVote 用户票数模型
type UserVote struct {
	Username  string    `json:"username"`
	Votes     int       `json:"votes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Ticket 票据模型
type Ticket struct {
	Value           string    `json:"value"`
	Version         string    `json:"version"`
	RemainingUsages int       `json:"remainingUsages"`
	ExpiresAt       time.Time `json:"expiresAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

// TicketHistory 票据历史记录
type TicketHistory struct {
	ID          int64     `json:"id"`
	Version     string    `json:"version"`
	TicketValue string    `json:"ticketValue"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiredAt   time.Time `json:"expiredAt"`
}

// VoteLog 投票日志
type VoteLog struct {
	ID            int64     `json:"id"`
	Username      string    `json:"username"`
	TicketVersion string    `json:"ticketVersion"`
	VotedAt       time.Time `json:"votedAt"`
}

// VoteRequest 投票请求
type VoteRequest struct {
	Usernames []string `json:"usernames"`
	Ticket    Ticket   `json:"ticket"`
}

// VoteResponse 投票响应
type VoteResponse struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	Usernames []string  `json:"usernames"`
	Timestamp time.Time `json:"timestamp"`
}

// VoteEvent Kafka投票事件
type VoteEvent struct {
	Usernames     []string  `json:"usernames"`
	TicketVersion string    `json:"ticketVersion"`
	VotedAt       time.Time `json:"votedAt"`
}
