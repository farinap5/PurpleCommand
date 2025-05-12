CODE = {
    PING = 1
}

function ping()
    local err = addtask(CODE.PING,"ping")
    if err then
        print("Error")
    end
end

-- impl, name, desc, func
command("impl","pits","ccc", ping)

function Main()
end