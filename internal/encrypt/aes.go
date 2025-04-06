package encrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
)

func AESPad(src []byte) []byte {
	padding := aes.BlockSize - len(src)%aes.BlockSize
	padData := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padData...)
}

func AESUnpad(src []byte) ([]byte, error) {
	length := len(src)
	padding := int(src[length-1])
	return src[:length-padding], nil
}

func (e Encrypt) AESCbcEncrypt(data []byte) []byte {
	data = AESPad(data)
	mode := cipher.NewCBCEncrypter(e.block, e.iv[:])
	cipherData := make([]byte, len(data))
	mode.CryptBlocks(cipherData, data)

	return cipherData
}

func (e Encrypt) AESCbcDecrypt(data []byte) ([]byte, error) {
	mode := cipher.NewCBCDecrypter(e.block, e.iv[:])
	plaintext := make([]byte, len(data))
	mode.CryptBlocks(plaintext, data)

	return AESUnpad(plaintext)
}
