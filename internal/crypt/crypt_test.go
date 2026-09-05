package crypt

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// 真实样例向量：来自网易加密存档 testdata/ESfjmffkJN0=.zip 的 db/CURRENT
// （密文去魔数后的 16 字节）与 db/MANIFEST-000006 文件名。
var sampleCurrentBody = []byte{
	0x75, 0x79, 0x7D, 0x7B, 0x7F, 0x7D, 0x66, 0x65,
	0x15, 0x08, 0x03, 0x02, 0x09, 0x08, 0x03, 0x3B,
}

const sampleManifest = "MANIFEST-000006"

func TestDeriveKey_SampleVector(t *testing.T) {
	currentEnc := append(append([]byte{}, Magic...), sampleCurrentBody...)
	key, err := DeriveKey(currentEnc, sampleManifest)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if string(key) != "88329851" {
		t.Fatalf("推导密钥 = %q, 期望 %q", key, "88329851")
	}
	if string(key) != string(DefaultKey) {
		t.Fatalf("推导密钥应等于官方默认密钥")
	}
}

func TestDeriveKey_HalvesMismatch(t *testing.T) {
	bad := append(append([]byte{}, Magic...), sampleCurrentBody...)
	bad[4+10] ^= 0xFF // 破坏后半 keystream
	if _, err := DeriveKey(bad, sampleManifest); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("期望 ErrKeyMismatch, 得到 %v", err)
	}
}

func TestDeriveKey_NotEncrypted(t *testing.T) {
	if _, err := DeriveKey([]byte("MANIFEST-000006\n"), sampleManifest); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("期望 ErrNotEncrypted, 得到 %v", err)
	}
}

func TestDeriveKey_OldMagic(t *testing.T) {
	data := append([]byte{}, OldMagic...)
	data = append(data, sampleCurrentBody...)
	if _, err := DeriveKey(data, sampleManifest); !errors.Is(err, ErrOldEncryption) {
		t.Fatalf("期望 ErrOldEncryption, 得到 %v", err)
	}
}

func TestDeriveKey_ShortBody(t *testing.T) {
	// 资源中心二次加密的特征之一是 CURRENT 约 80 字节；这里模拟过短的情形
	data := append(append([]byte{}, Magic...), []byte{1, 2, 3}...)
	if _, err := DeriveKey(data, sampleManifest); err == nil || errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("过短密文体应报长度错误, 得到 %v", err)
	}
}

func TestParseKey(t *testing.T) {
	// 无前缀一律 ASCII：默认密钥必须是 8 字节，而不是 4 字节 hex
	k, err := ParseKey("88329851")
	if err != nil || string(k) != "88329851" || len(k) != 8 {
		t.Fatalf(`ParseKey("88329851") = %q, %v; 期望 8 字节 ASCII`, k, err)
	}
	// hex: / 0x 前缀显式十六进制，两者都应得到与 ASCII 等价的 8 字节
	for _, in := range []string{"hex:3838333239383531", "0x3838333239383531", "0X3838333239383531"} {
		k, err = ParseKey(in)
		if err != nil || string(k) != "88329851" {
			t.Fatalf("ParseKey(%q) = %q, %v; 期望 ASCII 88329851", in, k, err)
		}
	}
	// 非 hex 字符按 ASCII
	k, err = ParseKey("testaaab")
	if err != nil || string(k) != "testaaab" {
		t.Fatalf("ParseKey ASCII: %q, %v", k, err)
	}
	// 非法输入
	if _, err = ParseKey("   "); err == nil {
		t.Fatal("空密钥应报错")
	}
	if _, err = ParseKey("hex:abc"); err == nil {
		t.Fatal("奇数长度 hex 应报错")
	}
	if _, err = ParseKey("hex:zz11"); err == nil {
		t.Fatal("非法 hex 字符应报错")
	}
}

func TestXORRoundTrip(t *testing.T) {
	data := []byte("hello, minecraft world! 这是一段测试文本。")
	enc := XOR(data, DefaultKey)
	if bytesEqual(enc, data) {
		t.Fatal("加密结果不应等于明文")
	}
	if got := XOR(enc, DefaultKey); !bytesEqual(got, data) {
		t.Fatalf("解密回读不一致: %q", got)
	}
}

func TestXORReaderPhase(t *testing.T) {
	// 模拟加密文件结构：4 字节魔数 + 密文；流式解密应与一次性 XOR 一致
	plain := []byte("0123456789abcdef0123456789abcdef")
	enc := EncryptBytes(plain, DefaultKey)
	r := io.MultiReader(skipBytes(enc, 4))
	got, err := io.ReadAll(NewXORReader(r, DefaultKey, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(got, plain) {
		t.Fatalf("流式解密不一致: %q", got)
	}
}

func TestEncryptDecryptFile(t *testing.T) {
	plain := []byte{0x0A, 0x00, 0x00, 0x00, 0x69, 0x0C}
	enc, err := EncryptFile(plain, DefaultKey)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc) {
		t.Fatal("加密结果应带魔数")
	}
	back, err := DecryptFile(enc, DefaultKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(back, plain) {
		t.Fatalf("回读不一致: % x", back)
	}
	if _, err := DecryptFile(plain, DefaultKey); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("明文应报 ErrNotEncrypted, 得到 %v", err)
	}
}

func TestPickManifest(t *testing.T) {
	got, err := PickManifest([]string{"MANIFEST-000002", "MANIFEST-000010", "MANIFEST-000006"})
	if err != nil || got != "MANIFEST-000010" {
		t.Fatalf("PickManifest = %q, %v; 期望最大序号", got, err)
	}
	if got, err = PickManifest([]string{"MANIFEST-abc", "MANIFEST-aaa"}); err != nil || got != "MANIFEST-abc" {
		t.Fatalf("非数字后缀应按字典序兜底: %q, %v", got, err)
	}
	if _, err = PickManifest([]string{"CURRENT", "000007.log"}); err == nil {
		t.Fatal("缺少 MANIFEST 应报错")
	}
}

// 测试辅助
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func EncryptBytes(plain, key []byte) []byte {
	out := append(append([]byte{}, Magic...), XOR(plain, key)...)
	return out
}

func skipBytes(b []byte, n int) io.Reader {
	return io.LimitReader(strings.NewReader(string(b[n:])), int64(len(b)-n))
}
