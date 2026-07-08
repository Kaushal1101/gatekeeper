-- KEYS[1]: bucket key
-- ARGV[1]: capacity (max tokens)
-- ARGV[2]: refill_rate (tokens per second)
-- ARGV[3]: cost (tokens this request consumes)
-- ARGV[4]: now (current time in milliseconds)
-- Returns: 1 if allowed, 0 if rejected

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

local data = redis.call("HMGET", key, "tokens", "last_refill")
local tokens = tonumber(data[1])
local last_refill = tonumber(data[2])

if tokens == nil then
    tokens = capacity
    last_refill = now
end

local elapsed = (now - last_refill) / 1000.0
local new_tokens = math.min(capacity, tokens + elapsed * refill_rate)

local ttl = math.ceil(capacity / refill_rate) * 2

if new_tokens < cost then
    redis.call("HSET", key, "tokens", new_tokens, "last_refill", now)
    redis.call("EXPIRE", key, ttl)
    return 0
end

redis.call("HSET", key, "tokens", new_tokens - cost, "last_refill", now)
redis.call("EXPIRE", key, ttl)
return 1
