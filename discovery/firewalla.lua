local cursor = '0'
local result = {}

repeat
    local scan_result = redis.call('SCAN', cursor, 'MATCH', 'host:mac:*', 'COUNT', 1000)
    cursor = scan_result[1]
    local keys = scan_result[2]

    for _, key in ipairs(keys) do
        -- Remove "host:mac:" prefix from key
        local mac = key:gsub('host:mac:', ''):lower()

        -- Get name field (user customized) or fallback to bname
        local name = redis.call('HGET', key, 'name') or redis.call('HGET', key, 'bname')

        if name then
            table.insert(result, '"' .. mac .. '": ["' .. name .. '"]')
        end
    end
until cursor == '0'

return '{' .. table.concat(result, ',') .. '}'