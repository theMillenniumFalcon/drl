package limiter

// KEYS[1]: rate-limit key, ARGV[1]: limit, ARGV[2]: window in seconds
// Returns {current_count, ttl_remaining, limit}
const slidingWindowScript = `
local current = redis.call("INCR", KEYS[1])
if current == 1 then
    redis.call("EXPIRE", KEYS[1], tonumber(ARGV[2]))
end
local ttl = redis.call("TTL", KEYS[1])
return {current, ttl, tonumber(ARGV[1])}
`

const resetScript = `
return redis.call("DEL", KEYS[1])
`
