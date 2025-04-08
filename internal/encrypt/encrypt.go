package encrypt

import (
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"io"
)

/*
	xor function for byte arrays.
	Both arrays must have the same length.
*/
func xor(d1, d2 [16]byte) [16]byte {
	var r [16]byte
	for i := range r {
		r[i] = d1[i] ^ d2[i]
	}
	return r
}

func EncryptInit() Encrypt {
	var key [16]byte
	io.ReadFull(rand.Reader, key[:])

	var iv [16]byte
	io.ReadFull(rand.Reader, iv[:])

	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err.Error())
	}

	data, _ := base64.StdEncoding.DecodeString("MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDWvbIed4UGs3VnQrYXv5SiocdmmHECq7lDPiv8Bf6o0ntDfmEZHAcactFOHyx7NkacjwJZ+lyeXIaRFmRuTDU5Yj8ekV3KSPjZNznwPjppBdA6V5GVXIZUiRYVyliY4dA8Iesn3NKNr52GLD5UxLv7MwIpXFDdl+o62pWTPMzDjQIDAQAB")
	parseResult, _ := x509.ParsePKIXPublicKey(data)

	return Encrypt{
		iv:      iv,
		block:   block,
		aeskey:  key,
		hmackey: xor(iv, key),

		RSAPublic: parseResult.(*rsa.PublicKey),
	}
}

/*
	May be used by the server to load config from
	the implant session.
*/
func EncryptImport(key, iv [16]byte) Encrypt {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err.Error())
	}
	data, err := base64.StdEncoding.DecodeString("MIICXQIBAAKBgQDWvbIed4UGs3VnQrYXv5SiocdmmHECq7lDPiv8Bf6o0ntDfmEZHAcactFOHyx7NkacjwJZ+lyeXIaRFmRuTDU5Yj8ekV3KSPjZNznwPjppBdA6V5GVXIZUiRYVyliY4dA8Iesn3NKNr52GLD5UxLv7MwIpXFDdl+o62pWTPMzDjQIDAQABAoGAPXmBx9IMbY4rcnu5GFRajzJEHL1QQOT7POJMAjKPJDJZYkmIL4GEERDElZo8CCvSDBiuoiaXpCg1x8xCxQahB323lIzL/t7wP1WQcqCzoxdqsZQ/G/mwv0hwAF1UZQHXEQnh+iKIM4zhqm1wwwqisjhAHMGkPXmGM3ioNKHdOWsCQQDrK1v6dF9XLNAKihgR+p6YtXOP3nO4nmAJ0M0dEjxDrTFcuxU3W5o/GMqd/PMH5DDe//7tbYlEq0v/Vv4mUlgDAkEA6cMbn+O5uz/pd1rooipUfDZmmPhQrxsFjsK3ykRROJHQRKbp1YnAqJ3nRBCtJCHPNrqJUtdAhBJqmQQmk4eJLwJAELfVYxmwyW67H3Svv190tOB5ZannyiEgLLJ2UnHAbQM79h6qpHPTpFar2M1prY7wVnoWcmSOFJ6k2XMiwDCsZwJBAOYCgZr4stcJUwqK2948snap/JfFtXYmq3hGJhuSzyxPZVM3vVvMuFHxVQ5HLmYgEkjykI5/mE6b5GF9kQuW0CcCQQCaiWft1EVRRp9CxCWOqbDzLEJ1n9QjZbZJ8V9QLiHFPd2848ZGfhyqw0W9+CAlFmk/wWjOVpA7BVg6tOk44911")
	if err != nil {
		panic(err.Error())
	}
	parseResult, err := x509.ParsePKCS1PrivateKey(data)
	if err != nil {
		panic(err.Error())
	}

	return Encrypt{
		aeskey: key,
		iv: iv,
		hmackey: xor(key, iv),

		block: block,
		RSAPrivate: parseResult,
	}
}

func (e Encrypt) EncryptGetKeys() ([16]byte, [16]byte) {
	return e.aeskey, e.iv
}

/*
	key  = x
	IV   = y
	hmac = x xor y
*/
