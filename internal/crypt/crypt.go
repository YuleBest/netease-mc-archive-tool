// Package crypt 实现网易我的世界（中国版）基岩存档的 XOR 加解密与密钥推导。
//
// 加密格式：加密文件 = 4 字节魔数 80 1D 30 01 + 原文与 8 字节密钥的循环逐字节异或。
// 密钥推导（已知明文攻击）：LevelDB 的 db/CURRENT 明文恒为 "MANIFEST-<序号>\n"，
// 恰为两个密钥周期，用其异或加密 CURRENT 去掉魔数后的密文即得密钥。
// 算法来源与完整验证记录见 docs/encryption.md。
package crypt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Magic 为现行加密格式的文件头魔数（大端 0x801D3001）。
var Magic = []byte{0x80, 0x1D, 0x30, 0x01}

// OldMagic 为旧版加密（AES-128-CFB8，密钥需调用网易 API）的文件头魔数。
var OldMagic = []byte{0x90, 0x1D, 0x30, 0x01}

// DefaultKey 为网易官方 XOREncryptDLL.dll 内置的默认密钥（ASCII）。
var DefaultKey = []byte("88329851")

var (
	// ErrOldEncryption 表示存档使用旧版 AES-CFB8 加密，无法离线解密。
	ErrOldEncryption = errors.New("旧版加密格式（魔数 90 1D 30 01，AES-CFB8）：密钥需调用网易 API 获取，无法离线解密")
	// ErrNotEncrypted 表示文件未带加密魔数。
	ErrNotEncrypted = errors.New("文件未带加密魔数 80 1D 30 01，不是网易 XOR 加密文件")
	// ErrKeyMismatch 表示由 CURRENT 推导的 keystream 前后两半不一致。
	ErrKeyMismatch = errors.New("密钥推导失败：keystream 前后两半不一致（存档可能损坏，或为资源中心二次加密）")
)

// IsEncrypted 报告 data 是否以现行加密魔数开头。
func IsEncrypted(data []byte) bool {
	return len(data) >= 4 && bytes.Equal(data[:4], Magic)
}

// HasOldMagic 报告 data 是否以旧版加密魔数开头。
func HasOldMagic(data []byte) bool {
	return len(data) >= 4 && bytes.Equal(data[:4], OldMagic)
}

// XOR 用 key 对 data 做循环逐字节异或。XOR 是自逆运算，加密与解密共用。
// key 不能为空，否则返回 nil。
func XOR(data, key []byte) []byte {
	if len(key) == 0 {
		return nil
	}
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return out
}

// NewXORReader 返回流式异或 Reader，从 phase 起始相位循环异或 key。
// phase 为该流首字节在完整数据中的偏移模 key 长度（解密时去掉 4 字节魔数后从 0 开始）。
// key 不能为空。
func NewXORReader(r io.Reader, key []byte, phase int) io.Reader {
	return &xorReader{r: r, key: key, phase: ((phase % len(key)) + len(key)) % len(key)}
}

type xorReader struct {
	r     io.Reader
	key   []byte
	phase int
}

func (x *xorReader) Read(p []byte) (int, error) {
	n, err := x.r.Read(p)
	for i := 0; i < n; i++ {
		p[i] ^= x.key[x.phase]
		x.phase++
		if x.phase == len(x.key) {
			x.phase = 0
		}
	}
	return n, err
}

// ParseKey 解析用户输入的密钥。
//
// 以 "hex:" 或 "0x" 前缀显式表示十六进制，否则一律按 ASCII 字节处理。
// 例如默认密钥 "88329851" 按预期解析为 8 字节 ASCII，而不会被误解为
// 4 字节十六进制（两者异或结果完全不同）。
func ParseKey(s string) ([]byte, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil, errors.New("密钥为空")
	}
	if strings.HasPrefix(t, "hex:") {
		return decodeHexKey(strings.TrimPrefix(t, "hex:"))
	}
	if strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X") {
		return decodeHexKey(t[2:])
	}
	return []byte(t), nil
}

func decodeHexKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s)%2 != 0 {
		return nil, fmt.Errorf("十六进制密钥长度须为偶数: %q", s)
	}
	key := make([]byte, len(s)/2)
	for i := 0; i < len(key); i++ {
		hi, ok1 := fromHexDigit(s[2*i])
		lo, ok2 := fromHexDigit(s[2*i+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("密钥含非法十六进制字符: %q", s)
		}
		key[i] = hi<<4 | lo
	}
	return key, nil
}

func fromHexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// DeriveKey 用已知明文攻击从加密的 CURRENT 文件内容与 MANIFEST 文件名恢复密钥。
//
// currentEnc 须为完整文件内容（含 4 字节魔数）；manifestName 为 db 目录中
// 序号最大的 MANIFEST-* 文件名（见 PickManifest）。LevelDB 规定 CURRENT 明文
// 恒为 manifestName + '\n'；正常情况下该明文长度 ≥ 16 字节且恰为两个密钥周期，
// 因此 keystream 前 8 字节与后 8 字节必须一致，不一致说明存档损坏或非标准加密。
func DeriveKey(currentEnc []byte, manifestName string) ([]byte, error) {
	if HasOldMagic(currentEnc) {
		return nil, ErrOldEncryption
	}
	if !IsEncrypted(currentEnc) {
		return nil, ErrNotEncrypted
	}
	plain := append([]byte(manifestName), '\n')
	body := currentEnc[4:]
	if len(body) < len(plain) {
		return nil, fmt.Errorf("CURRENT 加密体仅 %d 字节，不足以推导密钥（若为 80 字节左右，可能是资源中心二次加密）", len(body))
	}
	ks := XOR(body[:len(plain)], plain)
	// 前后两半一致性校验：至少比较 8 字节与剩余部分的重叠区
	overlap := len(plain) - 8
	if overlap > 8 {
		overlap = 8
	}
	if overlap <= 0 || !bytes.Equal(ks[:overlap], ks[8:8+overlap]) {
		return nil, ErrKeyMismatch
	}
	return ks[:8], nil
}

// PickManifest 从 db 目录的文件名中选出序号最大的 MANIFEST-*（即 CURRENT 指向的当前清单）。
func PickManifest(names []string) (string, error) {
	best := ""
	bestNum := uint64(0)
	hasNum := false
	for _, n := range names {
		if !strings.HasPrefix(n, "MANIFEST-") {
			continue
		}
		suf := n[len("MANIFEST-"):]
		if num, err := strconv.ParseUint(suf, 10, 64); err == nil {
			if !hasNum || num > bestNum {
				best, bestNum, hasNum = n, num, true
			}
			continue
		}
		// 无纯数字后缀的非标准命名：仅在尚无数字序号候选时按字典序兜底
		if !hasNum && n > best {
			best = n
		}
	}
	if best == "" {
		return "", errors.New("db 目录中找不到 MANIFEST-* 文件，无法推导密钥")
	}
	return best, nil
}

// DecryptFile 解密完整的加密文件内容（含 4 字节魔数），返回原始明文。
func DecryptFile(data, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("密钥为空")
	}
	if HasOldMagic(data) {
		return nil, ErrOldEncryption
	}
	if !IsEncrypted(data) {
		return nil, ErrNotEncrypted
	}
	return XOR(data[4:], key), nil
}

// EncryptFile 将明文加密为完整加密文件（4 字节魔数 + 密文）。
func EncryptFile(data, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("密钥为空")
	}
	out := make([]byte, 0, len(data)+len(Magic))
	out = append(out, Magic...)
	out = append(out, XOR(data, key)...)
	return out, nil
}
