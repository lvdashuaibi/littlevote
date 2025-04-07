local cjson = require("cjson")

-- 初始化全局计数器
success_counter = 0
fail_counter = 0

wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"

-- 原始 GraphQL 查询
local raw_mutation = [[
mutation TicketANDVoteTest {
  ticketAndVote(usernames: ["A"]) {
    success
    message
    usernames
    timestamp
  }
}
]]

-- 预处理查询语句（保留原始压缩逻辑）
local processed_query = raw_mutation
  :gsub("\n", " ")       -- 移除换行符
  :gsub("%s+", " ")      -- 合并连续空格
  :gsub('"', '\\"')      -- 转义双引号

-- 构建 JSON 请求体
wrk.body = string.format(
  '{"query": "%s"}',
  processed_query
)

