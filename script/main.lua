CODE = {
    PING = 1
}

function ping()
    print("command from script")
    local err = addtask(CODE.PING,"ping")
    if err then
        print("Error")
    end
end

-- impl, name, desc, func
command("impl","ping","ccc", ping)

function Main()
end
