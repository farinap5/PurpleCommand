package encrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

func AESPad(src []byte) []byte {
	padding := aes.BlockSize - len(src)%aes.BlockSize
	padData := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padData...)
}

func AESUnpad(src []byte) ([]byte, error) {
	length := len(src)
	if length == 0 {
		return nil, errors.New("AESUnpad: empty input")
	}
	padding := int(src[length-1])
	if padding == 0 || padding > aes.BlockSize || padding > length {
		return nil, errors.New("AESUnpad: invalid padding")
	}
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
