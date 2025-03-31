local log = require('log')

if not box.info.status then
    box.cfg{
        listen = '0.0.0.0:3301',
        log = 'file:tarantool.log',
        log_level = 6  
    }
else
    box.cfg{
        listen = '0.0.0.0:3301'
    }
end

local space_name = os.getenv('TARANTOOL_DATABASE') or 'polls'
if not box.space[space_name] then
    box.schema.space.create(space_name, {
        if_not_exists = true,
        format = {
            {'id', 'string'},
            {'creator', 'string'},
            {'question', 'string'},
            {'voters', 'map'},
            {'options', 'map'},
            {'is_closed', 'boolean'}
        }
    })
    box.space[space_name]:create_index('primary', {
        parts = {'id'},
        if_not_exists = true
    })
    log.info("Создан спейс: %s", space_name)
end

local user = os.getenv('TARANTOOL_USER')
local password = os.getenv('TARANTOOL_PASSWORD')

box.schema.user.drop(user, {if_exists = true})
box.schema.user.create(user, {
    password = password,
    if_not_exists = false
})

box.schema.user.grant(user, 'read,write,execute', 'universe')
log.info("Пользователь %s создан/обновлен", user)

log.info("Инициализация Tarantool завершена")