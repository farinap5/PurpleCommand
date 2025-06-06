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

function OnRegister(...)
    local args = {...}
    print("Name:", args[1])
    print("UUID:", args[2])
    print("Hostname:", args[3])
    print("User:", args[4])
    print("Socket:", args[5])
end

function OnCheck(...)
    local args = {...}
    print("Name:", args[1])
    print("UUID:", args[2])
    print("Hostname:", args[3])
    print("User:", args[4])
    print("data:", args[5])
    print("task:", args[6])
end

function OnResponse(...)
    local args = {...}
    print("Name:", args[1])
    print("UUID:", args[2])
    print("Hostname:", args[3])
    print("User:", args[4])
    print("response:", args[5])
    print("task:", args[6])
end

function Main()
end
