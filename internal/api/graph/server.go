package graph

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	graphql "github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/model"
	"github.com/lvdashuaibi/littlevote/internal/service"
)

// GraphQLServer GraphQL服务器
type GraphQLServer struct {
	schema   *graphql.Schema
	handler  *relay.Handler
	resolver *Resolver
}

// 读取GraphQL Schema定义
const schemaString = `
type UserVote {
  username: String!
  votes: Int!
  updatedAt: String!
}

type Ticket {
  value: String!
  version: String!
  remainingUsages: Int!
  expiresAt: String!
  createdAt: String!
}

type VoteResponse {
  success: Boolean!
  message: String!
  usernames: [String!]!
  timestamp: String!
}

input VoteInput {
  usernames: [String!]!
  ticket: TicketInput!
}

input TicketInput {
  value: String!
  version: String!
  remainingUsages: Int!
  expiresAt: String!
  createdAt: String!
}

type Query {
  # 获取当前票据
  getTicket: Ticket!
  
  # 查询用户票数
  getUserVotes(username: String!): UserVote!
  
  # 查询所有用户票数
  getAllUserVotes: [UserVote!]!
}

type Mutation {
  # 投票
  vote(input: VoteInput!): VoteResponse!
  
  # 获取票据并立即投票
  ticketAndVote(usernames: [String!]!): VoteResponse!
}

schema {
  query: Query
  mutation: Mutation
}
`

// NewGraphQLServer 创建新的GraphQL服务器
func NewGraphQLServer(voteService *service.VoteService) *GraphQLServer {
	resolver := NewResolver(voteService)

	// 解析Schema并创建GraphQL实例
	schema := graphql.MustParseSchema(schemaString, resolver,
		graphql.UseFieldResolvers(),
	)

	handler := &relay.Handler{Schema: schema}

	return &GraphQLServer{
		schema:   schema,
		handler:  handler,
		resolver: resolver,
	}
}

// Start 启动GraphQL服务器
func (s *GraphQLServer) Start(port int) error {
	// 创建路由
	mux := http.NewServeMux()

	// 设置GraphQL API端点
	mux.Handle(config.AppConfig.GraphQL.Path, s.handler)

	// 设置GraphQL Playground
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(playgroundHTML))
	})

	// 启动服务器
	addr := fmt.Sprintf(":%d", port)
	log.Printf("GraphQL服务已启动，API端点: %s, Playground: http://localhost%s/",
		config.AppConfig.GraphQL.Path, addr)

	return http.ListenAndServe(addr, mux)
}

// Resolver GraphQL解析器
type Resolver struct {
	voteService *service.VoteService
}

// NewResolver 创建新的解析器
func NewResolver(voteService *service.VoteService) *Resolver {
	return &Resolver{voteService: voteService}
}

// GetTicket 获取当前票据 ok
func (r *Resolver) GetTicket(ctx context.Context) (*TicketResolver, error) {
	failResponse := &TicketResolver{
		ticket: &model.Ticket{
			Value:           "",
			Version:         "",
			RemainingUsages: 0,
			ExpiresAt:       time.Now(),
			CreatedAt:       time.Now(),
		},
	}
	// 生成客户端ID
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())

	ticket, err := r.voteService.GetTicket(clientID)
	if err != nil {
		return failResponse, err
	}

	return &TicketResolver{ticket: ticket}, nil
}

// GetUserVotes 获取用户票数 ok
func (r *Resolver) GetUserVotes(ctx context.Context, args struct{ Username string }) (*UserVoteResolver, error) {
	failResponse := &UserVoteResolver{
		userVote: &model.UserVote{
			Username:  args.Username,
			Votes:     0,
			UpdatedAt: time.Now(),
		},
	}
	userVote, err := r.voteService.GetUserVote(args.Username)
	if err != nil {
		return failResponse, err
	}

	return &UserVoteResolver{userVote: userVote}, nil
}

// GetAllUserVotes 获取所有用户票数 delete
func (r *Resolver) GetAllUserVotes(ctx context.Context) ([]*UserVoteResolver, error) {
	userVotes, err := r.voteService.GetAllUserVotes()
	if err != nil {
		return nil, err
	}

	resolvers := make([]*UserVoteResolver, len(userVotes))
	for i, userVote := range userVotes {
		resolvers[i] = &UserVoteResolver{userVote: userVote}
	}

	return resolvers, nil
}

// Vote 投票
func (r *Resolver) Vote(ctx context.Context, args struct{ Input VoteInput }) (*VoteResponseResolver, error) {
	failResponse := &VoteResponseResolver{
		response: &model.VoteResponse{
			Success:   false,
			Message:   "投票失败",
			Usernames: args.Input.Usernames,
			Timestamp: time.Now(),
		},
	}
	fmt.Printf("failResponse: %v", failResponse.response)
	// 转换票据
	expiresAt, err := time.Parse(time.RFC3339, args.Input.Ticket.ExpiresAt)
	if err != nil {
		return failResponse, fmt.Errorf("解析票据过期时间失败: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339, args.Input.Ticket.CreatedAt)
	if err != nil {
		return failResponse, fmt.Errorf("解析票据创建时间失败: %w", err)
	}

	ticket := model.Ticket{
		Value:           args.Input.Ticket.Value,
		Version:         args.Input.Ticket.Version,
		RemainingUsages: int(args.Input.Ticket.RemainingUsages),
		ExpiresAt:       expiresAt,
		CreatedAt:       createdAt,
	}

	// 创建投票请求
	request := &model.VoteRequest{
		Usernames: args.Input.Usernames,
		Ticket:    ticket,
	}

	// 执行投票
	response, err := r.voteService.Vote(request)
	fmt.Printf("Vote: %v", response)
	if err != nil {
		fmt.Printf("Vote error: %v", err)
		fmt.Printf("Vote failed response: %v", failResponse.response)
		return failResponse, err
	}

	return &VoteResponseResolver{response: response}, nil
}

// TicketAndVote 获取票据并立即投票
func (r *Resolver) TicketAndVote(ctx context.Context, args struct{ Usernames []string }) (*VoteResponseResolver, error) {
	// 验证用户名列表非空
	if len(args.Usernames) == 0 {
		response := &model.VoteResponse{
			Success:   false,
			Message:   "投票失败: 用户名列表不能为空",
			Usernames: []string{},
			Timestamp: time.Now(),
		}
		return &VoteResponseResolver{response: response}, nil
	}

	// 验证用户名是否符合规范（A-Z）
	for _, username := range args.Usernames {
		if len(username) != 1 || username[0] < 'A' || username[0] > 'Z' {
			response := &model.VoteResponse{
				Success:   false,
				Message:   fmt.Sprintf("投票失败: 无效的用户名: %s, 用户名必须是A-Z之间的单个字母", username),
				Usernames: args.Usernames,
				Timestamp: time.Now(),
			}
			return &VoteResponseResolver{response: response}, nil
		}
	}

	// 调用服务方法
	response, err := r.voteService.TicketAndVote(args.Usernames)
	if err != nil {
		response = &model.VoteResponse{
			Success:   false,
			Message:   fmt.Sprintf("投票失败: %v", err),
			Usernames: args.Usernames,
			Timestamp: time.Now(),
		}
	}

	return &VoteResponseResolver{response: response}, nil
}

// TicketResolver 票据解析器
type TicketResolver struct {
	ticket *model.Ticket
}

func (r *TicketResolver) Value() string {
	return r.ticket.Value
}

func (r *TicketResolver) Version() string {
	return r.ticket.Version
}

func (r *TicketResolver) RemainingUsages() int32 {
	return int32(r.ticket.RemainingUsages)
}

func (r *TicketResolver) ExpiresAt() string {
	return r.ticket.ExpiresAt.Format(time.RFC3339)
}

func (r *TicketResolver) CreatedAt() string {
	return r.ticket.CreatedAt.Format(time.RFC3339)
}

// UserVoteResolver 用户票数解析器
type UserVoteResolver struct {
	userVote *model.UserVote
}

func (r *UserVoteResolver) Username() string {
	return r.userVote.Username
}

func (r *UserVoteResolver) Votes() int32 {
	return int32(r.userVote.Votes)
}

func (r *UserVoteResolver) UpdatedAt() string {
	return r.userVote.UpdatedAt.Format(time.RFC3339)
}

// VoteResponseResolver 投票响应解析器
type VoteResponseResolver struct {
	response *model.VoteResponse
}

func (r *VoteResponseResolver) Success() bool {
	return r.response.Success
}

func (r *VoteResponseResolver) Message() string {
	return r.response.Message
}

func (r *VoteResponseResolver) Usernames() []string {
	return r.response.Usernames
}

func (r *VoteResponseResolver) Timestamp() string {
	return r.response.Timestamp.Format(time.RFC3339)
}

// 投票输入类型
type VoteInput struct {
	Usernames []string
	Ticket    TicketInput
}

// 票据输入类型
type TicketInput struct {
	Value           string
	Version         string
	RemainingUsages int32
	ExpiresAt       string
	Holder          string
	CreatedAt       string
}

// playgroundHTML GraphQL Playground HTML
const playgroundHTML = `
<!DOCTYPE html>
<html>
<head>
  <meta charset=utf-8/>
  <meta name="viewport" content="user-scalable=no, initial-scale=1.0, minimum-scale=1.0, maximum-scale=1.0, minimal-ui">
  <title>Little Vote GraphQL Playground</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.22/build/static/css/index.css" />
  <link rel="shortcut icon" href="https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.22/build/favicon.png" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.22/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root">
    <style>
      body {
        background-color: rgb(23, 42, 58);
        font-family: Open Sans, sans-serif;
        height: 90vh;
      }
      #root {
        height: 100%;
        width: 100%;
        display: flex;
        align-items: center;
        justify-content: center;
      }
      .loading {
        font-size: 32px;
        font-weight: 200;
        color: rgba(255, 255, 255, .6);
        margin-left: 20px;
      }
      img {
        width: 78px;
        height: 78px;
      }
      .title {
        font-weight: 400;
      }
    </style>
    <img src='https://cdn.jsdelivr.net/npm/graphql-playground-react@1.7.22/build/logo.png' alt=''>
    <div class="loading"> 
      <span class="title">Little Vote GraphQL Playground</span>
    </div>
  </div>
  <script>window.addEventListener('load', function (event) {
      GraphQLPlayground.init(document.getElementById('root'), {
        endpoint: '/graphql'
      })
    })</script>
</body>
</html>
`
