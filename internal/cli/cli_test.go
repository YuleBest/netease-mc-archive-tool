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

// TestDecrypt_DirInput 验证目录形态输入（游戏原生存储形态）：
// 解压存档 zip 为目录后直接对其解密，世界名应取输入目录名。
func TestDecrypt_DirInput(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()

	// 解压
	zr, err := zip.OpenReader(testArchive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		p := filepath.Join(tmp, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	worldDir := filepath.Join(tmp, "ESfjmffkJN0=")
	out := runCmd(t, "decrypt", worldDir)
	if !strings.Contains(out, "(dir)") || !strings.Contains(out, "88329851") {
		t.Fatalf("目录解密输出异常:\n%s", out)
	}

	// 目录形态下 version / info 同样可用
	if out := runCmd(t, "version", filepath.Join(tmp, "ESfjmffkJN0=_decrypted")); !strings.Contains(out, "1.21.120") {
		t.Fatalf("目录形态 version 输出异常:\n%s", out)
	}
	out = runCmd(t, "info", filepath.Join(tmp, "ESfjmffkJN0=_decrypted"))
	if !strings.Contains(out, "世界:            ESfjmffkJN0=") {
		t.Fatalf("目录形态 info 世界名错误:\n%s", out)
	}
	if !strings.Contains(out, "未加密") {
		t.Fatalf("目录形态 info 应显示 db 未加密:\n%s", out)
	}
}

// TestTransform_SameInputOutputRejected 输出与输入同一路径必须被拒绝
// （否则 --overwrite 会在打开 zip 的同时截断/删除源存档）。
func TestTransform_SameInputOutputRejected(t *testing.T) {
	testArchiveAvailable(t)
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"decrypt", testArchive, "-o", testArchive, "--overwrite"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("输出与输入相同应报错")
	}
}

// TestExport_RealArchive 验证 export 命令：解密 + 世界内容提升到压缩包根。
func TestExport_RealArchive(t *testing.T) {
	testArchiveAvailable(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "world.mcworld")
	runCmd(t, "export", testArchive, "-o", out)

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	rootLevelDat := false
	var current []byte
	for _, f := range zr.File {
		if f.Name == "" {
			t.Fatal("导出产物不应包含空名条目（世界根目录条目剥离后应被跳过）")
		}
		switch f.Name {
		case "level.dat":
			rootLevelDat = true
		case "db/CURRENT":
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			current = data
		case strings.TrimSuffix(filepath.Base(testArchive), ".zip") + "/level.dat":
			t.Fatal("世界内容不应嵌套在子目录内")
		}
	}
	if !rootLevelDat {
		t.Fatal("level.dat 应位于压缩包根")
	}
	if string(current) != "MANIFEST-000006\n" {
		t.Fatalf("db/CURRENT 应为明文已知格式: %q", current)
	}

	// 导出的 .mcworld 可直接被 info/version 读取（世界位于压缩包根）
	if out := runCmd(t, "info", out); !strings.Contains(out, "1.21.120") || !strings.Contains(out, "我的世界") {
		t.Fatalf("info 读取 .mcworld 失败:\n%s", out)
	}
}
