-- KEYS[1]: base key
-- ARGV[1]: window_size (seconds)
-- ARGV[2]: limit (max cost units per window)
-- ARGV[3]: cost (cost units this request consumes)
-- ARGV[4]: now (current time in milliseconds)
-- Returns: 1 if allowed, 0 if rejected

local key = KEYS[1]
local window_size = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

local window_ms = window_size * 1000
local current_window = math.floor(now / window_ms) * window_ms
local prev_window = current_window - window_ms

local curr_key = key .. ":" .. current_window
local prev_key = key .. ":" .. prev_window

local curr_count = tonumber(redis.call("GET", curr_key)) or 0
local prev_count = tonumber(redis.call("GET", prev_key)) or 0

-- Weight previous window by how much of it falls within the current sliding window
local elapsed_fraction = (now - current_window) / window_ms
local effective_count = prev_count * (1.0 - elapsed_fraction) + curr_count

if effective_count + cost > limit then
    return 0
end

redis.call("INCRBY", curr_key, cost)
redis.call("EXPIRE", curr_key, window_size * 2)
return 1
