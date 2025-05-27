CODE = {
    PING = 1,
    SSH = 2
}

function ping()
    print("command ping from script")
    local err = addtask(CODE.PING, "ping")
    if err then
        print("Error")
    end
end

function ssh()
    print("command ssh from script")
    local err = addtask(CODE.SSH, "ssh")
    if err then
        print("Error")
    end
end

-- impl, name, desc, func
command("impl","ping","ccc", ping)
command("impl","ssh","ccc", ssh)

function Main()
end
