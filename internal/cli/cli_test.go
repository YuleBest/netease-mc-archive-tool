package cli

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试存档路径（个人数据已 gitignore；存在时才跑集成测试）。
const testArchive = "../../testdata/ESfjmffkJN0=.zip"

func testArchiveAvailable(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testArchive); err != nil {
		t.Skipf("测试存档不存在，跳过: %v", err)
	}
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("nmcat %s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

func TestVersionCommand_RealArchive(t *testing.T) {
	testArchiveAvailable(t)
	out := runCmd(t, "version", testArchive)
	if !strings.Contains(out, "1.21.120") {
		t.Fatalf("version 输出缺少 1.21.120:\n%s", out)
	}
	if !strings.Contains(out, "[1, 21, 120, 0, 0]") {
		t.Fatalf("version 输出缺少版本数组:\n%s", out)
	}
}

func TestInfoCommand_RealArchive(t *testing.T) {
	testArchiveAvailable(t)
	out := runCmd(t, "info", testArchive)
	for _, want := range []string{"我的世界", "1.21.120", "已加密", "88329851", "ESfjmffkJN0="} {
		if !strings.Contains(out, want) {
			t.Fatalf("info 输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestDecryptEncryptRoundTrip_RealArchive(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()
	decPath := filepath.Join(tmp, "dec.zip")
	runCmd(t, "decrypt", testArchive, "-o", decPath)
	encPath := filepath.Join(tmp, "enc.zip")
	runCmd(t, "encrypt", decPath, "-o", encPath)

	orig, err := zip.OpenReader(testArchive)
	if err != nil {
		t.Fatal(err)
	}
	defer orig.Close()
	re, err := zip.OpenReader(encPath)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()

	fileEntries := func(z *zip.ReadCloser) map[string][]byte {
		m := map[string][]byte{}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			r.Close()
			m[f.Name] = data
		}
		return m
	}
	a, b := fileEntries(orig), fileEntries(re)
	if len(a) != len(b) {
		t.Fatalf("条目数不一致: %d vs %d", len(a), len(b))
	}
	for name, da := range a {
		if !bytes.Equal(da, b[name]) {
			t.Fatalf("条目 %q 与原始加密存档不一致", name)
		}
	}

	// 解密产物中 CURRENT 应为已知明文
	dec, err := zip.OpenReader(decPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for _, f := range dec.File {
		if f.Name == "ESfjmffkJN0=/db/CURRENT" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			if string(data) != "MANIFEST-000006\n" {
				t.Fatalf("解密后 CURRENT = %q", data)
			}
		}
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"decrypt", testArchive, "-o", filepath.Join(tmp, "x.zip"), "-k", "WRONGKEY"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("错误密钥应失败")
	}
}
