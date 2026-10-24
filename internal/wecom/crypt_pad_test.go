package wecom

import (
	"bytes"
	"testing"
)

// TestCrypt_PaddingBlock32 覆盖企微 PKCS7 块大小 32 的边界：
// 明文长度使补齐值落在 17..32 时（旧实现按 16 块会解密失败）。
func TestCrypt_PaddingBlock32(t *testing.T) {
	// 测试用假凭据（不是任何真实环境的 token/AESKey）
	key := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	c, err := NewCrypt("test-token", key, "")
	if err != nil {
		t.Fatalf("NewCrypt: %v", err)
	}
	// 构造多种长度：使 payload(16+4+len+receiveid) 到 32 的补位分别落在各区段
	for n := 0; n < 40; n++ {
		msg := bytes.Repeat([]byte("x"), n)
		enc, err := c.Encrypt(msg)
		if err != nil {
			t.Fatalf("Encrypt(len=%d): %v", n, err)
		}
		got, receiveID, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt(len=%d): %v", n, err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("roundtrip len=%d mismatch: %q", n, got)
		}
		if receiveID != "" {
			t.Fatalf("receiveID=%q want empty", receiveID)
		}
	}
}

// TestCrypt_UnpadRejectsBadPad 校验非法补齐被拒绝。
func TestCrypt_UnpadRejectsBadPad(t *testing.T) {
	if _, err := pkcs7Unpad([]byte{1, 2, 3, 33}); err == nil {
		t.Fatal("pad=33 should be rejected")
	}
	if _, err := pkcs7Unpad([]byte{1, 2, 3, 0}); err == nil {
		t.Fatal("pad=0 should be rejected")
	}
	data := bytes.Repeat([]byte{7}, 32)
	data[31] = 17
	if _, err := pkcs7Unpad(data); err == nil {
		t.Fatal("inconsistent pad bytes should be rejected")
	}
}
