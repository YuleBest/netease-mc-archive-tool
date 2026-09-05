package level

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
)

// ---------------------------------------------------------------- 测试用小端 NBT 编码器

type enc struct{ buf bytes.Buffer }

func (e *enc) u1(v byte) { e.buf.WriteByte(v) }
func (e *enc) i16(v int16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(v))
	e.buf.Write(b[:])
}
func (e *enc) i32(v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	e.buf.Write(b[:])
}
func (e *enc) i64(v int64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	e.buf.Write(b[:])
}
func (e *enc) f32(v float32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
	e.buf.Write(b[:])
}
func (e *enc) f64(v float64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	e.buf.Write(b[:])
}
func (e *enc) name(s string) { e.i16(int16(len(s))); e.buf.WriteString(s) }
func (e *enc) raw(b []byte)  { e.buf.Write(b) }

// named 写入 [type][name] 头，payload 由调用方续写。
func (e *enc) named(t TagType, name string) { e.u1(byte(t)); e.name(name) }

// buildSampleNBT 构造一个覆盖全部标签类型的根 Compound。
func buildSampleNBT(t *testing.T) []byte {
	t.Helper()
	var e enc
	e.u1(byte(TagCompound))
	e.name("")
	e.named(TagString, "LevelName")
	e.name("我的世界")
	e.named(TagString, "InventoryVersion")
	e.name("1.21.120")
	// TAG_List<Int>
	e.named(TagList, "lastOpenedWithVersion")
	e.u1(byte(TagInt))
	e.i32(5)
	e.i32(1)
	e.i32(21)
	e.i32(120)
	e.i32(0)
	e.i32(0)
	// TAG_Int_Array
	e.named(TagIntArray, "minimumCompatibleClientVersion")
	e.i32(3)
	e.i32(1)
	e.i32(21)
	e.i32(0)
	e.named(TagInt, "GameType")
	e.i32(1)
	e.named(TagLong, "RandomSeed")
	e.i64(-6081985049543753511)
	e.named(TagLong, "LastPlayed")
	e.i64(1788662833000)
	e.named(TagByte, "commandsEnabled")
	e.u1(1)
	e.named(TagShort, "someShort")
	e.i16(-3)
	e.named(TagFloat, "someFloat")
	e.f32(1.5)
	e.named(TagDouble, "someDouble")
	e.f64(2.25)
	e.named(TagByteArray, "someBytes")
	e.i32(3)
	e.raw([]byte{9, 9, 9})
	e.named(TagLongArray, "someLongs")
	e.i32(2)
	e.i64(100)
	e.i64(-100)
	// 嵌套 Compound 与字符串 List
	e.named(TagCompound, "nested")
	e.named(TagString, "inner")
	e.name("值")
	e.u1(byte(TagEnd))
	e.named(TagList, "stringList")
	e.u1(byte(TagString))
	e.i32(2)
	e.name("a")
	e.name("b")
	e.u1(byte(TagEnd))
	return e.buf.Bytes()
}

func TestParse_AllTagTypes(t *testing.T) {
	raw := buildSampleNBT(t)
	name, root, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("根名 = %q", name)
	}
	checks := []struct {
		key  string
		want any
	}{
		{"LevelName", "我的世界"},
		{"InventoryVersion", "1.21.120"},
		{"GameType", int32(1)},
		{"RandomSeed", int64(-6081985049543753511)},
		{"LastPlayed", int64(1788662833000)},
		{"commandsEnabled", int8(1)},
		{"someShort", int16(-3)},
		{"someFloat", float32(1.5)},
		{"someDouble", float64(2.25)},
		{"someBytes", []byte{9, 9, 9}},
		{"someLongs", []int64{100, -100}},
	}
	for _, c := range checks {
		if c.want == nil {
			continue
		}
		got := root[c.key]
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, 期望 %#v", c.key, got, c.want)
		}
	}
	lv, ok := root["lastOpenedWithVersion"].([]any)
	if !ok || len(lv) != 5 {
		t.Fatalf("lastOpenedWithVersion 解析错误: %#v", root["lastOpenedWithVersion"])
	}
	if lv[2] != int32(120) {
		t.Fatalf("lastOpenedWithVersion[2] = %#v", lv[2])
	}
	ia, ok := root["minimumCompatibleClientVersion"].([]int32)
	if !ok || len(ia) != 3 || ia[2] != 0 {
		t.Fatalf("IntArray 解析错误: %#v", root["minimumCompatibleClientVersion"])
	}
	if _, ok := root["nested"].(map[string]any); !ok {
		t.Fatal("嵌套 Compound 解析错误")
	}
	if sl, ok := root["stringList"].([]any); !ok || len(sl) != 2 || sl[0] != "a" {
		t.Fatalf("字符串 List 解析错误: %#v", root["stringList"])
	}
}

func TestFormatVersionArray(t *testing.T) {
	cases := []struct {
		in   []int32
		want string
	}{
		{[]int32{1, 21, 120, 0, 0}, "1.21.120"},
		{[]int32{1, 21, 120, 5}, "1.21.120.5"},
		{[]int32{1, 21, 0}, "1.21.0"},
		{[]int32{2, 0, 0, 0}, "2.0.0"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := FormatVersionArray(c.in); got != c.want {
			t.Errorf("FormatVersionArray(%v) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestParseLevelDat_StandardHeader(t *testing.T) {
	nbt := buildSampleNBT(t)
	data := make([]byte, 8, 8+len(nbt))
	binary.LittleEndian.PutUint32(data[0:4], 10)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(nbt)))
	data = append(data, nbt...)

	ld, err := ParseLevelDat(data)
	if err != nil {
		t.Fatal(err)
	}
	if ld.StorageVersion != 10 {
		t.Fatalf("StorageVersion = %d", ld.StorageVersion)
	}
	info := ld.Info()
	if info.LevelName != "我的世界" || info.EngineVersion != "1.21.120" {
		t.Fatalf("Info 错误: %+v", info)
	}
	if info.MinimumCompatibleClientVersion != "1.21.0" {
		t.Fatalf("最小兼容版本 = %q", info.MinimumCompatibleClientVersion)
	}
	if info.GameType != 1 || info.RandomSeed != -6081985049543753511 || !info.CommandsEnabled {
		t.Fatalf("数值字段错误: %+v", info)
	}
	if info.LastPlayed != 1788662833000 {
		t.Fatalf("LastPlayed = %d", info.LastPlayed)
	}
}

func TestParseLevelDat_Gzip(t *testing.T) {
	var zbuf bytes.Buffer
	zw := gzip.NewWriter(&zbuf)
	if _, err := zw.Write(buildSampleNBT(t)); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	ld, err := ParseLevelDat(zbuf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if ld.Info().EngineVersion != "1.21.120" {
		t.Fatalf("gzip 路径解析错误: %+v", ld.Info())
	}
}

func TestParseLevelDat_Encrypted(t *testing.T) {
	enc, _ := crypt.EncryptFile([]byte("fake nbt"), crypt.DefaultKey)
	if _, err := ParseLevelDat(enc); err == nil || !errors.Is(err, crypt.ErrNotEncrypted) && !errors.Is(err, crypt.ErrOldEncryption) {
		t.Fatalf("加密 level.dat 应报错, 得到 %v", err)
	}
}

// TestParseLevelDat_RealArchive 用真实网易存档（若存在）做集成验证。
func TestParseLevelDat_RealArchive(t *testing.T) {
	const zipPath = "../../testdata/ESfjmffkJN0=.zip"
	if _, err := os.Stat(zipPath); err != nil {
		t.Skipf("测试存档不存在（.gitignore），跳过集成测试: %v", err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var raw []byte
	for _, f := range zr.File {
		if f.Name == "ESfjmffkJN0=/level.dat" {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			raw, err = io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if raw == nil {
		t.Fatal("zip 中找不到 level.dat")
	}
	ld, err := ParseLevelDat(raw)
	if err != nil {
		t.Fatal(err)
	}
	info := ld.Info()
	if info.EngineVersion != "1.21.120" {
		t.Fatalf("真实存档引擎版本 = %q, 期望 1.21.120", info.EngineVersion)
	}
	if info.LevelName != "我的世界" {
		t.Fatalf("真实存档世界名 = %q", info.LevelName)
	}
	lv := info.LastOpenedWithVersion
	if len(lv) != 5 || lv[0] != 1 || lv[1] != 21 || lv[2] != 120 {
		t.Fatalf("lastOpenedWithVersion = %v", lv)
	}
}
