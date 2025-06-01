CODE = {
    PING = 1,
    SSH = 2
}

function ping(payload)
    print("command ping from script args", payload)
    local err = addtask(CODE.PING, payload)
    if err then
        print("Error")
    end
end

function ssh(payload)
    print("command ssh from script with args", payload)
    local err = addtask(CODE.SSH, payload)
    if err then
        print("Error")
    end
end

-- impl, name, desc, func
command("impl","ping","Ping the implant", ping)
command("impl","ssh","Get an interactive session", ssh)

function Main()
end
