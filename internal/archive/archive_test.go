package archive

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
)

// 构造一个确定性的迷你加密存档用于测试。
const (
	testManifest = "MANIFEST-000042"
	testKey      = "TESTKEY1"
)

var testLogPlain = []byte("fake levelDB write-ahead log content ... 0123456789")

// makeWorldDir 在 dir 下写入明文世界目录并返回 db 内应被加密的文件内容映射。
func makeWorldDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	manifestPlain := append([]byte("leveldb.BytewiseComparator -- fake version edit payload"), make([]byte, 32)...)
	currentPlain := []byte(testManifest + "\n")
	logPlain := testLogPlain
	levelDat := append([]byte{0x0A, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00}, bytes.Repeat([]byte{0x11}, 0x20)...)

	mk := func(rel string, data []byte) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	encOr := func(plain []byte) []byte {
		enc, err := crypt.EncryptFile(plain, []byte(testKey))
		if err != nil {
			t.Fatal(err)
		}
		return enc
	}
	mk("world/level.dat", levelDat)
	mk("world/levelname.txt", []byte("测试世界\n"))
	mk("world/db/CURRENT", encOr(currentPlain))
	mk("world/db/"+testManifest, encOr(manifestPlain))
	mk("world/db/000007.log", logPlain)

	return map[string][]byte{
		"CURRENT":       currentPlain,
		testManifest:    manifestPlain,
		"000007.log":    logPlain,
		"level.dat":     levelDat,
		"levelname.txt": []byte("测试世界"),
	}
}

func makeWorldZip(t *testing.T, zipPath, srcDir string) {
	t.Helper()
	// 真实网易存档 zip 带顶层世界目录名，这里保留 srcDir 的基名作为前缀
	base := filepath.Base(srcDir)
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)
	err = filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, p)
		name := base + "/" + filepath.ToSlash(rel)
		if p == srcDir {
			_, err := zw.Create(name + "/")
			return err
		}
		if d.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func readZipEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatalf("zip 中不存在条目 %q", name)
	return nil
}

func TestOpenStore_Kinds(t *testing.T) {
	tmp := t.TempDir()
	makeWorldDir(t, tmp)
	src := filepath.Join(tmp, "world")
	zipPath := filepath.Join(tmp, "world.zip")
	makeWorldZip(t, zipPath, src)

	ds, err := OpenStore(src)
	if err != nil || ds.Kind() != "dir" {
		t.Fatalf("目录识别失败: %v %v", ds, err)
	}
	zs, err := OpenStore(zipPath)
	if err != nil || zs.Kind() != "zip" {
		t.Fatalf("zip 识别失败: %v %v", zs, err)
	}
	if closer, ok := zs.(interface{ Close() error }); ok {
		closer.Close()
	}
}

func TestInspect(t *testing.T) {
	tmp := t.TempDir()
	makeWorldDir(t, tmp)
	zipPath := filepath.Join(tmp, "world.zip")
	makeWorldZip(t, zipPath, filepath.Join(tmp, "world"))

	store, err := OpenStore(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zs := store.(*ZipStore)
	defer zs.Close()

	st, err := Inspect(store)
	if err != nil {
		t.Fatal(err)
	}
	if st.DBPrefix != "world/db" || st.WorldRoot != "world" {
		t.Fatalf("db 定位错误: %q %q", st.DBPrefix, st.WorldRoot)
	}
	if st.FileCount != 5 || st.DirCount != 2 {
		t.Fatalf("条目统计错误: files=%d dirs=%d", st.FileCount, st.DirCount)
	}
	// 期望总大小直接按磁盘上的源目录求和，避免手工数错
	var wantSize int64
	var wantFiles int
	filepath.WalkDir(filepath.Join(tmp, "world"), func(p string, d os.DirEntry, err error) error { //nolint:errcheck
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				wantSize += info.Size()
				wantFiles++
			}
		}
		return nil
	})
	if st.FileCount != wantFiles {
		t.Fatalf("条目统计错误: files=%d, 期望 %d", st.FileCount, wantFiles)
	}
	if st.TotalSize != wantSize {
		t.Fatalf("总大小错误: %d, 期望 %d", st.TotalSize, wantSize)
	}
	if st.LevelDatPath != "world/level.dat" {
		t.Fatalf("level.dat 定位错误: %q", st.LevelDatPath)
	}
	if st.LevelNameTxt != "测试世界" {
		t.Fatalf("levelname.txt 读取错误: %q", st.LevelNameTxt)
	}
	if len(st.EncryptedDB) != 2 || st.DBFiles != 3 {
		t.Fatalf("加密统计错误: %v db=%d", st.EncryptedDB, st.DBFiles)
	}
}

func TestTransform_DecryptZip(t *testing.T) {
	tmp := t.TempDir()
	makeWorldDir(t, tmp)
	zipPath := filepath.Join(tmp, "world.zip")
	makeWorldZip(t, zipPath, filepath.Join(tmp, "world"))

	store, err := OpenStore(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.(*ZipStore).Close()
	sink, err := NewSink(filepath.Join(tmp, "dec.zip"), true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Transform(store, sink, Options{Mode: ModeDecrypt})
	if err != nil {
		t.Fatal(err)
	}
	if res.KeySource != "自动推导" || string(res.Key) != testKey {
		t.Fatalf("密钥推导错误: %q %q", res.Key, res.KeySource)
	}
	if len(res.Transformed) != 2 {
		t.Fatalf("应恰好转换 CURRENT 与 MANIFEST: %v", res.Transformed)
	}
	if got := readZipEntry(t, filepath.Join(tmp, "dec.zip"), "world/db/CURRENT"); string(got) != testManifest+"\n" {
		t.Fatalf("CURRENT 明文错误: %q", got)
	}
	if got := readZipEntry(t, filepath.Join(tmp, "dec.zip"), "world/db/"+testManifest); !bytes.Contains(got, []byte("leveldb.BytewiseComparator")) {
		t.Fatal("MANIFEST 应含比较器字符串")
	}
	if got := readZipEntry(t, filepath.Join(tmp, "dec.zip"), "world/db/000007.log"); !bytes.Equal(got, testLogPlain) {
		t.Fatal("明文 WAL 不应被改动")
	}
	if got := readZipEntry(t, filepath.Join(tmp, "dec.zip"), "world/level.dat"); !bytes.Equal(got[:8], []byte{0x0A, 0, 0, 0, 0x20, 0, 0, 0}) {
		t.Fatal("level.dat 不应被改动")
	}
}

func TestTransform_RoundTripZip(t *testing.T) {
	tmp := t.TempDir()
	makeWorldDir(t, tmp)
	orig := filepath.Join(tmp, "world.zip")
	makeWorldZip(t, orig, filepath.Join(tmp, "world"))

	// 解密 → 加密 → 与原始逐条目字节比对
	decPath := filepath.Join(tmp, "dec.zip")
	runTransform(t, orig, decPath, Options{Mode: ModeDecrypt})
	encPath := filepath.Join(tmp, "enc.zip")
	runTransform(t, decPath, encPath, Options{Mode: ModeEncrypt, Key: []byte(testKey)})

	a, _ := zip.OpenReader(orig)
	defer a.Close()
	b, _ := zip.OpenReader(encPath)
	defer b.Close()
	if len(a.File) != len(b.File) {
		t.Fatalf("条目数不一致: %d vs %d", len(a.File), len(b.File))
	}
	for i, fa := range a.File {
		fb := b.File[i]
		if fa.Name != fb.Name {
			t.Fatalf("条目顺序/名称不一致: %q vs %q", fa.Name, fb.Name)
		}
		da, _ := readAllFile(fa)
		db, _ := readAllFile(fb)
		if !bytes.Equal(da, db) {
			t.Fatalf("条目 %q 内容不一致", fa.Name)
		}
	}
}

func TestTransform_WrongKey(t *testing.T) {
	tmp := t.TempDir()
	makeWorldDir(t, tmp)
	zipPath := filepath.Join(tmp, "world.zip")
	makeWorldZip(t, zipPath, filepath.Join(tmp, "world"))

	store, err := OpenStore(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.(*ZipStore).Close()
	sink, err := NewSink(filepath.Join(tmp, "dec.zip"), true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Transform(store, sink, Options{Mode: ModeDecrypt, Key: []byte("WRONGKEY")})
	if err == nil || !strings.Contains(err.Error(), "密钥不正确") {
		t.Fatalf("错误密钥应被 CURRENT 校验拦截, 得到 %v", err)
	}
}

func TestTransform_OldMagicAborts(t *testing.T) {
	tmp := t.TempDir()
	makeWorldDir(t, tmp)
	// 替换 CURRENT 为旧版魔数
	cur := filepath.Join(tmp, "world", "db", "CURRENT")
	old := append(append([]byte{}, crypt.OldMagic...), bytes.Repeat([]byte{0x42}, 16)...)
	if err := os.WriteFile(cur, old, 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(tmp, "world.zip")
	makeWorldZip(t, zipPath, filepath.Join(tmp, "world"))

	store, err := OpenStore(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.(*ZipStore).Close()
	sink, err := NewSink(filepath.Join(tmp, "dec.zip"), true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Transform(store, sink, Options{Mode: ModeDecrypt})
	if err == nil {
		t.Fatal("旧版加密应中止")
	}
}

func runTransform(t *testing.T, in, out string, opts Options) *Result {
	t.Helper()
	store, err := OpenStore(in)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := store.(interface{ Close() error }); ok {
		defer c.Close()
	}
	sink, err := NewSink(out, strings.HasSuffix(out, ".zip"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Transform(store, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readAllFile(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// fakeStore 用于测试 StripWorldRoot 的名称映射与过滤。
type fakeStore struct{ entries []Entry }

func (f *fakeStore) Kind() string              { return "fake" }
func (f *fakeStore) Entries() ([]Entry, error) { return f.entries, nil }
func (f *fakeStore) Open(name string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("content-of-" + name)), nil
}
func (f *fakeStore) ReadAll(name string) ([]byte, error) {
	return []byte("content-of-" + name), nil
}

func TestStripWorldRoot(t *testing.T) {
	store := &fakeStore{entries: []Entry{
		{Name: "world/db/CURRENT", Size: 16},
		{Name: "world/db/", IsDir: true},
		{Name: "world/level.dat", Size: 32},
		{Name: "outside.txt", Size: 1},
	}}
	stripped, err := StripWorldRoot(store, "world")
	if err != nil {
		t.Fatal(err)
	}
	es, err := stripped.Entries()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(es))
	for _, e := range es {
		got = append(got, e.Name)
	}
	want := []string{"db/", "db/CURRENT", "level.dat"}
	if len(got) != len(want) {
		t.Fatalf("剥离后条目 = %v, 期望 %v（world 子树外的 outside.txt 应被过滤）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("剥离后条目 = %v, 期望 %v", got, want)
		}
	}
	data, err := stripped.ReadAll("db/CURRENT")
	if err != nil || string(data) != "content-of-world/db/CURRENT" {
		t.Fatalf("剥离后读取应映射回原始路径: %q, %v", data, err)
	}
	// 根级世界原样返回
	same, err := StripWorldRoot(store, "")
	if err != nil || same != Store(store) {
		t.Fatalf("worldRoot 为空应原样返回: %v %v", same, err)
	}
}

func TestResolveExportOutput(t *testing.T) {
	tmp := t.TempDir()
	zipIn := filepath.Join(tmp, "ESfjmffkJN0=.zip")
	if err := os.WriteFile(zipIn, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirIn := filepath.Join(tmp, "world")
	if err := os.MkdirAll(dirIn, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		in, flag, want string
	}{
		{zipIn, "", filepath.Join(tmp, "ESfjmffkJN0=.mcworld")},
		{zipIn, filepath.Join(tmp, "b", "out"), filepath.Join(tmp, "b", "out.mcworld")},
		{zipIn, filepath.Join(tmp, "b", "out.mcworld"), filepath.Join(tmp, "b", "out.mcworld")},
		{dirIn, "", filepath.Join(tmp, "world.mcworld")},
	}
	for _, c := range cases {
		got, err := ResolveExportOutput(c.in, c.flag)
		if err != nil {
			t.Fatalf("ResolveExportOutput(%q,%q): %v", c.in, c.flag, err)
		}
		if got != filepath.Clean(c.want) {
			t.Errorf("ResolveExportOutput(%q,%q) = %q, 期望 %q", c.in, c.flag, got, c.want)
		}
	}
}
