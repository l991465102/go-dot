local function redis_type(key)
  local value = redis.call('TYPE', key)
  if type(value) == 'table' then
    return value['ok']
  end
  return value
end

local function decimal_at_most(value, limit)
  if string.len(value) < string.len(limit) then
    return true
  end
  if string.len(value) > string.len(limit) then
    return false
  end
  return value <= limit
end

local function valid_redis_integer_for_increment(key)
  local value = redis.call('GET', key)
  if not value then
    return true
  end
  local negative = string.sub(value, 1, 1) == '-'
  local digits = value
  local limit = '9223372036854775807'
  if negative then
    digits = string.sub(value, 2)
    limit = '9223372036854775808'
  end
  if digits == '' or not string.match(digits, '^%d+$') then
    return false
  end
  -- Redis integer parsing accepts zero or a canonical, non-zero decimal.
  if (string.len(digits) > 1 and string.sub(digits, 1, 1) == '0') or (negative and digits == '0') then
    return false
  end
  if not decimal_at_most(digits, limit) then
    return false
  end
  -- INCR would overflow only at the positive signed 64-bit maximum.
  return negative or digits ~= '9223372036854775807'
end

local function valid_hll(key)
  local ok, _ = pcall(redis.call, 'PFCOUNT', key)
  return ok
end

if redis.call('EXISTS', KEYS[1]) == 1 then
  return 0
end

-- Redis scripts are atomic but runtime errors do not roll back earlier writes.
-- Validate every key type before changing any counter.
if ARGV[3] == '1' then
  local hour_pv_type = redis_type(KEYS[2])
  local day_pv_type = redis_type(KEYS[3])
  if (hour_pv_type ~= 'none' and hour_pv_type ~= 'string') or
     (day_pv_type ~= 'none' and day_pv_type ~= 'string') then
    return redis.error_reply('WRONGTYPE realtime PV key')
  end
  if not valid_redis_integer_for_increment(KEYS[2]) or
     not valid_redis_integer_for_increment(KEYS[3]) then
    return redis.error_reply('INVALID realtime PV integer')
  end
end

if ARGV[4] == '1' then
  local expected_uv_type = 'string'
  if ARGV[5] == 'EXACT' then
    expected_uv_type = 'set'
  end
  local hour_uv_type = redis_type(KEYS[4])
  local day_uv_type = redis_type(KEYS[5])
  if (hour_uv_type ~= 'none' and hour_uv_type ~= expected_uv_type) or
     (day_uv_type ~= 'none' and day_uv_type ~= expected_uv_type) then
    return redis.error_reply('WRONGTYPE realtime UV key')
  end
  if ARGV[5] ~= 'EXACT' and (not valid_hll(KEYS[4]) or not valid_hll(KEYS[5])) then
    return redis.error_reply('INVALID realtime HLL key')
  end
end

if ARGV[3] == '1' then
  redis.call('INCR', KEYS[2])
  redis.call('INCR', KEYS[3])
  redis.call('EXPIRE', KEYS[2], ARGV[1])
  redis.call('EXPIRE', KEYS[3], ARGV[1])
end

if ARGV[4] == '1' then
  if ARGV[5] == 'EXACT' then
    redis.call('SADD', KEYS[4], ARGV[2])
    redis.call('SADD', KEYS[5], ARGV[2])
  else
    redis.call('PFADD', KEYS[4], ARGV[2])
    redis.call('PFADD', KEYS[5], ARGV[2])
  end
  redis.call('EXPIRE', KEYS[4], ARGV[1])
  redis.call('EXPIRE', KEYS[5], ARGV[1])
end

-- The completion marker is deliberately last. An error above remains retryable.
redis.call('SET', KEYS[1], '1', 'EX', ARGV[1])
return 1
