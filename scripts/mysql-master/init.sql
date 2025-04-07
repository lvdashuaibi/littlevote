-- 创建用户表
CREATE TABLE IF NOT EXISTS `user_votes` (
  `username` CHAR(1) NOT NULL,
  `votes` INT NOT NULL DEFAULT 0,
  `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 插入预设用户A-Z
INSERT INTO `user_votes` (`username`, `votes`) VALUES
('A', 0), ('B', 0), ('C', 0), ('D', 0), ('E', 0),
('F', 0), ('G', 0), ('H', 0), ('I', 0), ('J', 0),
('K', 0), ('L', 0), ('M', 0), ('N', 0), ('O', 0),
('P', 0), ('Q', 0), ('R', 0), ('S', 0), ('T', 0),
('U', 0), ('V', 0), ('W', 0), ('X', 0), ('Y', 0),
('Z', 0);

-- 创建票据历史表
CREATE TABLE IF NOT EXISTS `ticket_history` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `version` VARCHAR(64) NOT NULL,
  `ticket_value` VARCHAR(128) NOT NULL,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `expired_at` TIMESTAMP NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_version` (`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 创建当前活跃票据表
CREATE TABLE IF NOT EXISTS `tickets` (
  `version` VARCHAR(64) NOT NULL,
  `value` VARCHAR(128) NOT NULL,
  `remaining_usages` INT NOT NULL,
  `expires_at` TIMESTAMP NOT NULL,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`version`),
  INDEX `idx_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 创建投票日志表
CREATE TABLE IF NOT EXISTS `vote_logs` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `username` CHAR(1) NOT NULL,
  `ticket_version` VARCHAR(64) NOT NULL,
  `voted_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  INDEX `idx_username` (`username`),
  INDEX `idx_ticket_version` (`ticket_version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 创建复制用户
CREATE USER 'repl'@'%' IDENTIFIED BY 'repl';
GRANT REPLICATION SLAVE ON *.* TO 'repl'@'%';
GRANT ALL PRIVILEGES ON littlevote.* TO 'repl'@'%';
FLUSH PRIVILEGES;

-- 允许root用户远程连接
DROP USER IF EXISTS 'root'@'%';
CREATE USER 'root'@'%' IDENTIFIED BY 'root';
GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' WITH GRANT OPTION;
FLUSH PRIVILEGES;
