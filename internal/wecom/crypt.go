package wecom

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // 企微官方签名算法要求 SHA1
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
)

var (
	ErrMalformedAESKey   = errors.New("malformed encoding_aes_key")
	ErrInvalidSignature  = errors.New("invalid msg_signature")
	ErrDecryptFailed     = errors.New("decrypt failed")
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
)

// Crypt 企业微信消息加解密（EncodingAESKey + Token + CorpID/ReceiveID）。
type Crypt struct {
	token  string
	aesKey []byte
	corpID string
	rand   io.Reader
}

func NewCrypt(token, encodingAESKey, corpID string) (*Crypt, error) {
	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		return nil, err
	}
	return &Crypt{
		token:  token,
		aesKey: key,
		corpID: corpID,
		rand:   rand.Reader,
	}, nil
}

func (c *Crypt) WithRand(r io.Reader) *Crypt {
	c.rand = r
	return c
}

func decodeAESKey(encodingAESKey string) ([]byte, error) {
	// 官方 EncodingAESKey 为 43 字符，补 '=' 后 base64 得到 32 字节
	raw, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedAESKey, err)
	}
	if len(raw) != 32 {
		return nil, ErrMalformedAESKey
	}
	return raw, nil
}

// Sign 计算 msg_signature = sha1(sort(token, timestamp, nonce, encrypt)).
func (c *Crypt) Sign(timestamp, nonce, encrypt string) string {
	return makeSignature(c.token, timestamp, nonce, encrypt)
}

// Verify 校验请求签名。
func (c *Crypt) Verify(msgSignature, timestamp, nonce, encrypt string) bool {
	want := c.Sign(timestamp, nonce, encrypt)
	return subtle.ConstantTimeCompare([]byte(want), []byte(msgSignature)) == 1
}

func makeSignature(parts ...string) string {
	tmp := append([]string(nil), parts...)
	sort.Strings(tmp)
	h := sha1.New() //nolint:gosec
	for _, p := range tmp {
		_, _ = h.Write([]byte(p))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Decrypt 解密 base64 密文，返回明文消息与 ReceiveID。
func (c *Crypt) Decrypt(base64Msg string) (msg []byte, receiveID string, err error) {
	buf, err := base64.StdEncoding.DecodeString(base64Msg)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidCiphertext, err)
	}
	if len(buf)%aes.BlockSize != 0 || len(buf) < aes.BlockSize {
		return nil, "", ErrInvalidCiphertext
	}
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return nil, "", err
	}
	iv := c.aesKey[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(buf, buf)
	buf, err = pkcs7Unpad(buf)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	if len(buf) < 20 {
		return nil, "", ErrInvalidCiphertext
	}
	msgLen := binary.BigEndian.Uint32(buf[16:20])
	if int(20+msgLen) > len(buf) {
		return nil, "", ErrInvalidCiphertext
	}
	msg = buf[20 : 20+msgLen]
	receiveID = string(buf[20+msgLen:])
	return msg, receiveID, nil
}

// Encrypt 加密明文，ReceiveID 使用 CorpID。
func (c *Crypt) Encrypt(msg []byte) (string, error) {
	payload := make([]byte, 16, 16+4+len(msg)+len(c.corpID))
	if _, err := io.ReadFull(c.rand, payload); err != nil {
		return "", err
	}
	payload = payload[:cap(payload)]
	binary.BigEndian.PutUint32(payload[16:], uint32(len(msg)))
	copy(payload[20:], msg)
	copy(payload[20+len(msg):], []byte(c.corpID))
	padded := pkcs7Pad(payload)

	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return "", err
	}
	iv := c.aesKey[:16]
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(padded, padded)
	return base64.StdEncoding.EncodeToString(padded), nil
}

func pkcs7Pad(data []byte) []byte {
	pad := aes.BlockSize - len(data)%aes.BlockSize
	if pad == 0 {
		pad = aes.BlockSize
	}
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, ErrInvalidCiphertext
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, ErrInvalidCiphertext
	}
	for i := 0; i < pad; i++ {
		if data[len(data)-1-i] != byte(pad) {
			return nil, ErrInvalidCiphertext
		}
	}
	return data[:len(data)-pad], nil
}
