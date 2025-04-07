package repository

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/model"
)

type MySQLRepository struct {
	masterDB *sql.DB
	slaveDB  *sql.DB
}

func NewMySQLRepository() (*MySQLRepository, error) {
	masterDB, err := sql.Open("mysql", config.AppConfig.MySQL.Master)
	if err != nil {
		return nil, fmt.Errorf("连接主数据库失败: %w", err)
	}

	masterDB.SetMaxOpenConns(config.AppConfig.MySQL.MaxOpenConns)
	masterDB.SetMaxIdleConns(config.AppConfig.MySQL.MaxIdleConns)
	masterDB.SetConnMaxLifetime(time.Hour)

	if err = masterDB.Ping(); err != nil {
		return nil, fmt.Errorf("主数据库连接测试失败: %w", err)
	}

	slaveDB, err := sql.Open("mysql", config.AppConfig.MySQL.Slave)
	if err != nil {
		return nil, fmt.Errorf("连接从数据库失败: %w", err)
	}

	slaveDB.SetMaxOpenConns(config.AppConfig.MySQL.MaxOpenConns)
	slaveDB.SetMaxIdleConns(config.AppConfig.MySQL.MaxIdleConns)
	slaveDB.SetConnMaxLifetime(time.Hour)

	if err = slaveDB.Ping(); err != nil {
		log.Printf("从数据库连接测试失败: %v，将使用主数据库代替", err)
		slaveDB = masterDB
	}

	return &MySQLRepository{
		masterDB: masterDB,
		slaveDB:  slaveDB,
	}, nil
}

// GetUserVote 获取用户票数
func (r *MySQLRepository) GetUserVote(username string) (*model.UserVote, error) {
	query := "SELECT username, votes, updated_at FROM user_votes WHERE username = ?"
	row := r.slaveDB.QueryRow(query, username)

	var userVote model.UserVote
	err := row.Scan(&userVote.Username, &userVote.Votes, &userVote.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("用户 %s 不存在", username)
		}
		return nil, fmt.Errorf("查询用户票数失败: %w", err)
	}

	return &userVote, nil
}

// GetAllUserVotes 获取所有用户票数
func (r *MySQLRepository) GetAllUserVotes() ([]*model.UserVote, error) {
	query := "SELECT username, votes, updated_at FROM user_votes ORDER BY username"
	rows, err := r.slaveDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("查询所有用户票数失败: %w", err)
	}
	defer rows.Close()

	var userVotes []*model.UserVote
	for rows.Next() {
		var userVote model.UserVote
		if err := rows.Scan(&userVote.Username, &userVote.Votes, &userVote.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描用户票数失败: %w", err)
		}
		userVotes = append(userVotes, &userVote)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代用户票数失败: %w", err)
	}

	return userVotes, nil
}

// IncrementVotes 增加用户票数
func (r *MySQLRepository) IncrementVotes(usernames []string, ticketVersion string) error {
	tx, err := r.masterDB.Begin()
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}

	// 更新用户票数
	incrementStmt, err := tx.Prepare("UPDATE user_votes SET votes = votes + 1 WHERE username = ?")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("准备更新票数语句失败: %w", err)
	}
	defer incrementStmt.Close()

	// 记录投票日志
	logStmt, err := tx.Prepare("INSERT INTO vote_logs (username, ticket_version) VALUES (?, ?)")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("准备投票日志语句失败: %w", err)
	}
	defer logStmt.Close()

	// 执行投票操作
	for _, username := range usernames {
		// 更新票数
		result, err := incrementStmt.Exec(username)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("更新用户 %s 票数失败: %w", username, err)
		}

		// 检查是否找到用户
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("获取更新结果失败: %w", err)
		}
		if rowsAffected == 0 {
			tx.Rollback()
			return fmt.Errorf("用户 %s 不存在", username)
		}

		// 插入投票日志
		_, err = logStmt.Exec(username, ticketVersion)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("记录用户 %s 投票日志失败: %w", username, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// SaveTicketHistory 保存票据历史
func (r *MySQLRepository) SaveTicketHistory(ticketHistory *model.TicketHistory) error {
	query := "INSERT INTO ticket_history (version, ticket_value, created_at, expired_at) VALUES (?, ?, ?, ?)"
	_, err := r.masterDB.Exec(query,
		ticketHistory.Version,
		ticketHistory.TicketValue,
		ticketHistory.CreatedAt,
		ticketHistory.ExpiredAt,
	)
	if err != nil {
		return fmt.Errorf("保存票据历史失败: %w", err)
	}
	return nil
}

// SaveTicket 保存当前活跃票据
func (r *MySQLRepository) SaveTicket(ticket *model.Ticket) error {
	query := `INSERT INTO tickets (version, value, remaining_usages, expires_at) 
			 VALUES (?, ?, ?, ?) 
			 ON DUPLICATE KEY UPDATE 
			 value = VALUES(value), 
			 remaining_usages = VALUES(remaining_usages), 
			 expires_at = VALUES(expires_at)`

	_, err := r.masterDB.Exec(query,
		ticket.Version,
		ticket.Value,
		ticket.RemainingUsages,
		ticket.ExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("保存票据到MySQL失败: %w", err)
	}
	return nil
}

// DecrementTicketUsage 减少票据使用次数
func (r *MySQLRepository) DecrementTicketUsage(version string) (int, error) {
	// 开始事务
	tx, err := r.masterDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("开始事务失败: %w", err)
	}

	// 获取当前使用次数
	var remainingUsages int
	query := "SELECT remaining_usages FROM tickets WHERE version = ? FOR UPDATE"
	err = tx.QueryRow(query, version).Scan(&remainingUsages)
	if err != nil {
		tx.Rollback()
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("票据不存在")
		}
		return 0, fmt.Errorf("查询票据使用次数失败: %w", err)
	}

	// 检查是否还有剩余使用次数
	if remainingUsages <= 0 {
		tx.Rollback()
		return 0, fmt.Errorf("票据使用次数已耗尽")
	}

	// 减少使用次数
	remainingUsages--
	updateQuery := "UPDATE tickets SET remaining_usages = ? WHERE version = ?"
	_, err = tx.Exec(updateQuery, remainingUsages, version)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("更新票据使用次数失败: %w", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}

	return remainingUsages, nil
}

// GetTicket 获取当前活跃票据
func (r *MySQLRepository) GetTicket(version string) (*model.Ticket, error) {
	query := `SELECT version, value, remaining_usages, expires_at, created_at 
			 FROM tickets 
			 WHERE version = ?`

	var ticket model.Ticket
	err := r.slaveDB.QueryRow(query, version).Scan(
		&ticket.Version,
		&ticket.Value,
		&ticket.RemainingUsages,
		&ticket.ExpiresAt,
		&ticket.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("票据不存在")
		}
		return nil, fmt.Errorf("获取票据失败: %w", err)
	}

	return &ticket, nil
}

// GetNewestTicketVersion 获取最新的票据版本
func (r *MySQLRepository) GetNewestTicketVersion() (string, error) {
	query := `SELECT version FROM tickets 
			  WHERE expires_at > NOW() 
			  ORDER BY created_at DESC 
			  LIMIT 1`

	var version string
	err := r.slaveDB.QueryRow(query).Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // 没有有效票据
		}
		return "", fmt.Errorf("获取最新票据版本失败: %w", err)
	}

	return version, nil
}

// Close 关闭数据库连接
func (r *MySQLRepository) Close() {
	if r.masterDB != nil {
		r.masterDB.Close()
	}
	if r.slaveDB != nil && r.slaveDB != r.masterDB {
		r.slaveDB.Close()
	}
}
