-- 导入必要的Lua库
local http = require("socket.http")
local ltn12 = require("ltn12")
local cjson = require("cjson")

-- 定义测试的GraphQL端点（根据实际配置调整端口和路径）
local graphql_url = "http://localhost:8080/graphql"

-- 构建GraphQL请求内容
local mutation = [[
mutation TicketANDVoteTest {
  ticketAndVote(usernames: ["C", "D"]) {
    success
    message
    usernames
    timestamp
  }
}
]]

-- 编码请求体为JSON格式
local request_body = cjson.encode({
    query = mutation:gsub("\n", " "):gsub("%s+", " ") -- 压缩查询语句
})

-- 配置HTTP请求头
local headers = {
    ["Content-Type"] = "application/json",
    ["Content-Length"] = tostring(#request_body)
}

-- 记录请求信息
print("====== 发送请求 ======")
print("URL:", graphql_url)
print("Headers:", cjson.encode(headers))
print("Body:", request_body)
print("\n")

-- 发送HTTP POST请求
local response_body = {}
local res, code, response_headers = http.request{
    url = graphql_url,
    method = "POST",
    headers = headers,
    source = ltn12.source.string(request_body),
    sink = ltn12.sink.table(response_body)
}

-- 将响应内容合并为字符串
response_body = table.concat(response_body)

-- 记录响应信息
print("====== 收到响应 ======")
print("HTTP状态码:", code)
print("响应头:", cjson.encode(response_headers))
print("响应内容:")
print(response_body)
print("\n")

-- 解析响应内容
local ok, response_data = pcall(cjson.decode, response_body)
if not ok then
    print("!!! JSON解析失败:", response_data)
    return
end

-- 处理响应结果
if response_data.data == nil then
    print("!!! 请求失败 - 服务端返回空数据")
    if response_data.errors then
        print("错误详情:")
        for _, err in ipairs(response_data.errors) do
            print(string.format("[%s] %s", err.extensions.code, err.message))
        end
    end
else
    -- 提取并展示响应数据
    local result = response_data.data.ticketAndVote
    print("*** 请求成功 - 响应数据解析:")
    print(string.format("操作结果: %s", result.success and "成功" or "失败"))
    print("返回信息:", result.message)
    print("投票用户:", table.concat(result.usernames, ", "))
    print("时间戳:", result.timestamp)
end