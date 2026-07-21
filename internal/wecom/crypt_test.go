package wecom

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// 固定 43 字符 EncodingAESKey（+ = 后 32 字节）
const testAESKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"

func TestCrypt_EncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCrypt("mytoken", testAESKey, "wwCORP")
	if err != nil {
		t.Fatalf("new crypt: %v", err)
	}
	// 固定随机前缀
	fixed := bytes.Repeat([]byte{0x11}, 16)
	c.WithRand(bytes.NewReader(append(fixed, make([]byte, 1024)...)))

	plain := []byte(`<xml><MsgType><![CDATA[event]]></MsgType></xml>`)
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// 用真随机再测一轮
	c2, _ := NewCrypt("mytoken", testAESKey, "wwCORP")
	c2.rand = rand.Reader
	enc2, err := c2.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt2: %v", err)
	}
	got, recv, err := c2.Decrypt(enc2)
	if err != nil {
		t.Fatalf("decrypt2: %v", err)
	}
	if string(got) != string(plain) || recv != "wwCORP" {
		t.Fatalf("got=%q recv=%q", got, recv)
	}
	_ = enc
}

func TestCrypt_VerifyAndEcho(t *testing.T) {
	c, err := NewCrypt("mytoken", testAESKey, "wwCORP")
	if err != nil {
		t.Fatal(err)
	}
	msgSig, echostr, err := BuildEchoQuery(c, "hello-echo", "1409659813", "1372623149")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Verify(msgSig, "1409659813", "1372623149", echostr) {
		t.Fatal("verify failed")
	}
	if c.Verify("deadbeef", "1409659813", "1372623149", echostr) {
		t.Fatal("bad signature should fail")
	}
	plain, _, err := c.Decrypt(echostr)
	if err != nil || string(plain) != "hello-echo" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
}

func TestCrypt_MalformedKey(t *testing.T) {
	if _, err := NewCrypt("t", "short", "c"); err == nil {
		t.Fatal("expected malformed key")
	}
}
