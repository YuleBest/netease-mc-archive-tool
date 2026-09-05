// Package archive 提供网易存档的统一读写抽象：支持 zip 压缩包或目录两种形态，
// 自动定位 LevelDB 的 db 目录，并以流式方式对条目做转换（解密 / 加密 / 原样复制），
// 大文件不会整体载入内存。
package archive

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
)

// Entry 为存档中的一个条目。Name 使用 '/' 分隔，目录条目以 '/' 结尾。
type Entry struct {
	Name  string
	Size  int64
	IsDir bool
}

// Store 为只读的存档源（zip 或目录）。
type Store interface {
	// Kind 返回来源类型："zip" 或 "dir"。
	Kind() string
	// Entries 返回全部条目，按名称排序（含目录条目）。
	Entries() ([]Entry, error)
	// Open 打开指定文件条目的读取流。
	Open(name string) (io.ReadCloser, error)
	// ReadAll 读取指定文件条目的全部内容（用于 CURRENT 等小文件）。
	ReadAll(name string) ([]byte, error)
}

// Sink 为存档输出目标。
type Sink interface {
	// CreateDir 创建目录条目（zip 中保留显式目录项）。
	CreateDir(name string) error
	// CreateFile 创建文件条目并返回写入流。
	CreateFile(e Entry) (io.WriteCloser, error)
	// Close 完成写出（zip 需写 central directory）。
	Close() error
}

// ---------------------------------------------------------------- Store

// OpenStore 按内容自动识别输入：zip 文件（PK 魔数）或目录。
func OpenStore(path string) (Store, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return &DirStore{root: path}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var magic [4]byte
	_, err = io.ReadFull(f, magic[:])
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: 不是可识别的存档（无法读取文件头）", path)
	}
	if magic == [4]byte{'P', 'K', 0x03, 0x04} {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, fmt.Errorf("打开 zip 失败: %w", err)
		}
		return &ZipStore{path: path, zr: zr}, nil
	}
	return nil, fmt.Errorf("%s: 不是 zip 压缩包，也不是目录", path)
}

// ZipStore 从 zip 压缩包读取。
type ZipStore struct {
	path string
	zr   *zip.ReadCloser
}

func (s *ZipStore) Kind() string { return "zip" }

func (s *ZipStore) Close() error { return s.zr.Close() }

func (s *ZipStore) Entries() ([]Entry, error) {
	var out []Entry
	for _, f := range s.zr.File {
		out = append(out, Entry{
			Name:  filepath.ToSlash(f.Name),
			Size:  int64(f.UncompressedSize64),
			IsDir: f.FileInfo().IsDir(),
		})
	}
	sortEntries(out)
	return out, nil
}

func (s *ZipStore) Open(name string) (io.ReadCloser, error) {
	for _, f := range s.zr.File {
		if filepath.ToSlash(f.Name) == name {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("zip 中不存在条目 %q", name)
}

func (s *ZipStore) ReadAll(name string) ([]byte, error) {
	r, err := s.Open(name)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// DirStore 从目录读取。
type DirStore struct {
	root string
}

func (s *DirStore) Kind() string { return "dir" }

func (s *DirStore) Entries() ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(s.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == s.root {
			return nil
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			out = append(out, Entry{Name: name + "/", IsDir: true})
			return nil
		}
		out = append(out, Entry{Name: name, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortEntries(out)
	return out, nil
}

func (s *DirStore) Open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.root, filepath.FromSlash(name)))
}

func (s *DirStore) ReadAll(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.root, filepath.FromSlash(name)))
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].Name < es[j].Name })
}

// ---------------------------------------------------------------- Sink

// NewSink 创建输出目标。asZip 为 true 时 path 视为 zip 文件，否则视为目录。
// 调用方需自行保证 path 不存在（或已获准覆盖）。
func NewSink(path string, asZip bool) (Sink, error) {
	if asZip {
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		return &ZipSink{f: f, w: zip.NewWriter(f)}, nil
	}
	return &DirSink{root: path}, nil
}

type ZipSink struct {
	f *os.File
	w *zip.Writer
}

func (s *ZipSink) CreateDir(name string) error {
	// 目录条目在 zip 中以 "/" 结尾的 header 名表示
	_, err := s.w.Create(name)
	return err
}

func (s *ZipSink) CreateFile(e Entry) (io.WriteCloser, error) {
	w, err := s.w.Create(e.Name)
	if err != nil {
		return nil, err
	}
	return nopWriteCloser{w}, nil
}

// nopWriteCloser 适配 zip.Writer 的无 Close 写入流。
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (s *ZipSink) Close() error {
	if err := s.w.Close(); err != nil {
		return err
	}
	return s.f.Close()
}

type DirSink struct {
	root string
}

func (s *DirSink) CreateDir(name string) error {
	return os.MkdirAll(filepath.Join(s.root, filepath.FromSlash(name)), 0o755)
}

func (s *DirSink) CreateFile(e Entry) (io.WriteCloser, error) {
	p := filepath.Join(s.root, filepath.FromSlash(e.Name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return os.Create(p)
}

func (s *DirSink) Close() error { return nil }

// ---------------------------------------------------------------- 定位与统计

// LocateDB 在条目列表中定位 LevelDB 目录（含 CURRENT 文件的层级）。
// 返回其父目录前缀（"" 表示存档根目录）。
func LocateDB(entries []Entry) (string, error) {
	for _, e := range entries {
		if !e.IsDir && path.Base(e.Name) == "CURRENT" {
			return path.Dir(e.Name), nil
		}
	}
	return "", fmt.Errorf("找不到 db/CURRENT，不是有效的网易版存档（或已解密且结构异常）")
}

// WorldRoot 返回 db 所在世界的根目录前缀（"" 表示根）。
func WorldRoot(dbPrefix string) string {
	return path.Dir(dbPrefix)
}

// IsUnderDB 报告条目名是否位于 db 目录内。
func IsUnderDB(name, dbPrefix string) bool {
	if dbPrefix == "" {
		return true
	}
	return strings.HasPrefix(name, dbPrefix+"/")
}

// ManifestNames 返回 db 目录内全部 MANIFEST-* 文件名（不含路径）。
func ManifestNames(entries []Entry, dbPrefix string) []string {
	var out []string
	for _, e := range entries {
		if e.IsDir || !IsUnderDB(e.Name, dbPrefix) {
			continue
		}
		base := path.Base(e.Name)
		if strings.HasPrefix(base, "MANIFEST-") {
			out = append(out, base)
		}
	}
	return out
}

// WorldStats 为只读检查得到的存档概况。
type WorldStats struct {
	Kind         string   // zip / dir
	DBPrefix     string   // db 目录前缀（"" 表示根）
	WorldRoot    string   // 世界根目录前缀（"" 表示根）
	FileCount    int      // 文件条目数
	DirCount     int      // 目录条目数
	TotalSize    int64    // 文件总字节数
	LevelDatPath string   // level.dat 的条目路径（"" 表示未找到）
	LevelNameTxt string   // levelname.txt 内容（去首尾空白），"" 表示不存在
	DBFiles      int      // db 内文件数
	EncryptedDB  []string // db 内带现行加密魔数的文件
	EncryptedOld []string // db 内带旧版魔数的文件
}

// Inspect 只读扫描存档，收集统计信息（不产生输出）。
func Inspect(store Store) (*WorldStats, error) {
	entries, err := store.Entries()
	if err != nil {
		return nil, err
	}
	dbPrefix, err := LocateDB(entries)
	if err != nil {
		return nil, err
	}
	st := &WorldStats{
		Kind:      store.Kind(),
		DBPrefix:  dbPrefix,
		WorldRoot: WorldRoot(dbPrefix),
	}
	levelDatDepth := -1
	for _, e := range entries {
		if e.IsDir {
			st.DirCount++
			continue
		}
		st.FileCount++
		st.TotalSize += e.Size
		base := path.Base(e.Name)
		if IsUnderDB(e.Name, dbPrefix) {
			st.DBFiles++
			head, err := readHead(store, e.Name, 4)
			if err != nil {
				return nil, err
			}
			switch {
			case crypt.IsEncrypted(head):
				st.EncryptedDB = append(st.EncryptedDB, base)
			case crypt.HasOldMagic(head):
				st.EncryptedOld = append(st.EncryptedOld, base)
			}
			continue
		}
		if base == "level.dat" && (levelDatDepth < 0 || len(strings.Split(e.Name, "/")) < levelDatDepth) {
			st.LevelDatPath = e.Name
			levelDatDepth = len(strings.Split(e.Name, "/"))
		}
		if base == "levelname.txt" {
			b, err := store.ReadAll(e.Name)
			if err != nil {
				return nil, err
			}
			st.LevelNameTxt = strings.TrimSpace(string(b))
		}
	}
	return st, nil
}

// readHead 读取条目开头至多 n 字节。
func readHead(store Store, name string, n int) ([]byte, error) {
	r, err := store.Open(name)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	br := bufio.NewReader(r)
	head := make([]byte, n)
	m, err := io.ReadFull(br, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return head[:m], nil
}

// ---------------------------------------------------------------- 转换

// Mode 为转换方向。
type Mode int

const (
	// ModeDecrypt 解密 db 内所有带魔数的文件。
	ModeDecrypt Mode = iota
	// ModeEncrypt 加密 db 内 CURRENT / MANIFEST-* / *.ldb（与游戏写入行为一致）。
	ModeEncrypt
)

// Options 为 Transform 的参数。
type Options struct {
	Mode Mode
	// Key 为加密/解密密钥。解密模式下留空表示自动从 CURRENT+MANIFEST 推导。
	Key []byte
}

// Result 为一次转换的摘要。
type Result struct {
	DBPrefix    string
	Key         []byte
	KeySource   string // "自动推导" / "指定" / "默认"
	Transformed []string
	Copied      int
	Skipped     []string // 加密模式下跳过的已加密文件
	Notes       []string // 软性提示（如 MANIFEST 校验信息）
}

// Transform 把 store 的全部条目流式写入 sink，按模式解密或加密 db 内文件。
func Transform(store Store, sink Sink, opts Options) (*Result, error) {
	entries, err := store.Entries()
	if err != nil {
		return nil, err
	}
	dbPrefix, err := LocateDB(entries)
	if err != nil {
		return nil, err
	}
	manifestName, err := crypt.PickManifest(ManifestNames(entries, dbPrefix))
	if err != nil {
		return nil, err
	}

	currentPath := joinName(dbPrefix, "CURRENT")
	currentRaw, err := store.ReadAll(currentPath)
	if err != nil {
		return nil, err
	}

	res := &Result{DBPrefix: dbPrefix, Key: opts.Key, KeySource: "指定"}
	switch {
	case opts.Mode == ModeDecrypt && len(opts.Key) == 0:
		key, err := crypt.DeriveKey(currentRaw, manifestName)
		if err != nil {
			return nil, err
		}
		res.Key, res.KeySource = key, "自动推导"
	case opts.Mode == ModeDecrypt:
		// 用户指定密钥：用 CURRENT 的已知明文校验，错误密钥立即失败
		plain, err := crypt.DecryptFile(currentRaw, res.Key)
		if err != nil {
			return nil, err
		}
		if string(plain) != manifestName+"\n" {
			return nil, fmt.Errorf("密钥不正确：CURRENT 校验失败（明文应为 %q）", manifestName+"\n")
		}
	case opts.Mode == ModeEncrypt:
		// 加密自检：CURRENT 明文加密后再解密必须与原文一致
		enc, err := crypt.EncryptFile(currentRaw, res.Key)
		if err != nil {
			return nil, err
		}
		back, err := crypt.DecryptFile(enc, res.Key)
		if err != nil || !bytes.Equal(back, currentRaw) {
			return nil, fmt.Errorf("加密自检失败（CURRENT 回读不一致）")
		}
	}

	for _, e := range entries {
		if e.IsDir {
			if err := sink.CreateDir(e.Name); err != nil {
				return nil, err
			}
			continue
		}
		changed, skipped, err := transformEntry(store, sink, e, dbPrefix, opts, res)
		if err != nil {
			return nil, err
		}
		switch {
		case changed:
			res.Transformed = append(res.Transformed, path.Base(e.Name))
			res.Copied++
		case skipped:
			res.Skipped = append(res.Skipped, path.Base(e.Name))
			res.Copied++
		default:
			res.Copied++
		}
	}

	// 解密后对 MANIFEST 做软校验：标准 LevelDB 清单中应含比较器名字符串
	if opts.Mode == ModeDecrypt {
		manPath := joinName(dbPrefix, manifestName)
		if manRaw, err := store.ReadAll(manPath); err == nil && crypt.IsEncrypted(manRaw) {
			plain := crypt.XOR(manRaw[4:], res.Key)
			if !bytes.Contains(plain, []byte("leveldb.BytewiseComparator")) {
				res.Notes = append(res.Notes, "提示：MANIFEST 解密结果中未找到 leveldb.BytewiseComparator，请核对存档是否可正常打开")
			}
		}
	}

	if err := sink.Close(); err != nil {
		return nil, err
	}
	return res, nil
}

// transformEntry 处理单个文件条目，返回（是否转换，是否跳过转换，错误）。
func transformEntry(store Store, sink Sink, e Entry, dbPrefix string, opts Options, res *Result) (bool, bool, error) {
	if !IsUnderDB(e.Name, dbPrefix) || e.Size == 0 {
		return false, false, copyEntry(store, sink, e)
	}
	r, err := store.Open(e.Name)
	if err != nil {
		return false, false, err
	}
	defer r.Close()
	br := bufio.NewReaderSize(r, 64<<10)
	head, err := br.Peek(4)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, false, err
	}

	if opts.Mode == ModeDecrypt {
		switch {
		case crypt.HasOldMagic(head):
			return false, false, fmt.Errorf("%s: %w", path.Base(e.Name), crypt.ErrOldEncryption)
		case crypt.IsEncrypted(head):
			if _, err := br.Discard(4); err != nil {
				return false, false, err
			}
			w, err := sink.CreateFile(e)
			if err != nil {
				return false, false, err
			}
			defer w.Close()
			if _, err := io.Copy(w, crypt.NewXORReader(br, res.Key, 0)); err != nil {
				return false, false, err
			}
			return true, false, nil
		}
		return false, false, copyFrom(sink, e, br)
	}

	// ModeEncrypt：与游戏行为一致，仅加密 CURRENT / MANIFEST-* / *.ldb
	if !shouldEncrypt(path.Base(e.Name)) {
		return false, false, copyFrom(sink, e, br)
	}
	if crypt.IsEncrypted(head) {
		return false, true, copyFrom(sink, e, br)
	}
	w, err := sink.CreateFile(e)
	if err != nil {
		return false, false, err
	}
	defer w.Close()
	if _, err := w.Write(crypt.Magic); err != nil {
		return false, false, err
	}
	if _, err := io.Copy(w, crypt.NewXORReader(br, res.Key, 0)); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// shouldEncrypt 报告 db 内该文件名是否应被加密（与网易游戏行为一致）。
func shouldEncrypt(base string) bool {
	return base == "CURRENT" ||
		strings.HasPrefix(base, "MANIFEST-") ||
		strings.HasSuffix(base, ".ldb")
}

func copyEntry(store Store, sink Sink, e Entry) error {
	r, err := store.Open(e.Name)
	if err != nil {
		return err
	}
	defer r.Close()
	return copyFrom(sink, e, r)
}

func copyFrom(sink Sink, e Entry, r io.Reader) error {
	w, err := sink.CreateFile(e)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func joinName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}
