// https://groups.google.com/g/gorilla-web/c/VjXmApL1qA8

package agent

import (
	"embed"
	"log"
	"purpcmd/utils"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

func CallWSServer(remoteAdd string, key embed.FS) {
	var t time.Duration = 1
	var c int = 0
	for {
		err := wsclient(remoteAdd, key)
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

func wsclient(remoteAdd string, key embed.FS) error {
	log.Printf("Connecting to ws://%s/", remoteAdd)

	wclient, _, err := websocket.DefaultDialer.Dial("ws://"+remoteAdd+"/", nil)
	if err != nil {
		return err
	}
	defer wclient.Close()

	// create new connect file
	// "New" from adapter to use websock as net.Conn
	webSockConn := utils.New(wclient)
	defer webSockConn.Close()

	s := Session{
		AuthKeys: make(map[string]bool),
	}

	PubKey, _ := key.ReadFile("utils/key/id_ecdsa.pub")
	s.PubKey, _, _, _, err = ssh.ParseAuthorizedKey(PubKey)
	utils.Err(err)

	// Keep the fingerprint for authentication
	s.AuthKeys[FingerprintKey(s.PubKey)] = true

	config := &ssh.ServerConfig{
		PublicKeyCallback: s.pubCallBack, // Challenge with pubkey
	}

	privKey, _ := key.ReadFile("utils/key/id_ecdsa")
	pkey, err := ssh.ParsePrivateKey(privKey)
	utils.Err(err)
	config.AddHostKey(pkey)

	conn, chans, reqs, err := ssh.NewServerConn(webSockConn, config)
	utils.Err(err)
	go ssh.DiscardRequests(reqs)
	s.HandServerConn(conn.Permissions.Extensions["x"], chans)
	return nil
}
