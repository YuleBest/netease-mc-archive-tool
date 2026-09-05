// Package level 实现基岩版（Bedrock Edition）小端 NBT 读取与 level.dat 解析，
// 用于从存档中提取 MC 引擎版本号与世界基本信息。
package level

import (
	"encoding/binary"
	"fmt"
	"math"
)

// TagType 为 NBT 标签类型编号（Bedrock 与 Java 版编号一致，但字节序为小端）。
type TagType uint8

// NBT 标签类型常量。
const (
	TagEnd       TagType = 0
	TagByte      TagType = 1
	TagShort     TagType = 2
	TagInt       TagType = 3
	TagLong      TagType = 4
	TagFloat     TagType = 5
	TagDouble    TagType = 6
	TagByteArray TagType = 7
	TagString    TagType = 8
	TagList      TagType = 9
	TagCompound  TagType = 10
	TagIntArray  TagType = 11
	TagLongArray TagType = 12
)

// Parse 解析一段小端 NBT 数据，返回根 Compound 的名称与内容。
// 数值类型映射为 Go 原生类型：int8/int16/int32/int64/float32/float64，
// 复合结构为 map[string]any 与 []any。
func Parse(data []byte) (string, map[string]any, error) {
	r := &reader{buf: data}
	t, err := r.tag()
	if err != nil {
		return "", nil, err
	}
	if t != TagCompound {
		return "", nil, fmt.Errorf("NBT 根标签不是 Compound（type=%d）", t)
	}
	name, err := r.string()
	if err != nil {
		return "", nil, err
	}
	v, err := r.payload(TagCompound)
	if err != nil {
		return "", nil, err
	}
	root, _ := v.(map[string]any)
	return name, root, nil
}

type reader struct {
	buf []byte
	pos int
}

func (r *reader) read(n int) ([]byte, error) {
	if r.pos+n > len(r.buf) {
		return nil, fmt.Errorf("NBT 数据在第 %d 字节处越界（需 %d 字节）", r.pos, n)
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *reader) u1() (uint8, error) {
	b, err := r.read(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *reader) tag() (TagType, error) {
	v, err := r.u1()
	return TagType(v), err
}

func (r *reader) string() (string, error) {
	b, err := r.read(2)
	if err != nil {
		return "", err
	}
	n := int(binary.LittleEndian.Uint16(b))
	s, err := r.read(n)
	if err != nil {
		return "", err
	}
	return string(s), nil
}

func (r *reader) payload(t TagType) (any, error) {
	switch t {
	case TagByte:
		b, err := r.u1()
		return int8(b), err
	case TagShort:
		b, err := r.read(2)
		return int16(binary.LittleEndian.Uint16(b)), err
	case TagInt:
		b, err := r.read(4)
		return int32(binary.LittleEndian.Uint32(b)), err
	case TagLong:
		b, err := r.read(8)
		return int64(binary.LittleEndian.Uint64(b)), err
	case TagFloat:
		b, err := r.read(4)
		return math.Float32frombits(binary.LittleEndian.Uint32(b)), err
	case TagDouble:
		b, err := r.read(8)
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), err
	case TagByteArray:
		b, err := r.read(4)
		if err != nil {
			return nil, err
		}
		n := int(int32(binary.LittleEndian.Uint32(b)))
		return r.read(n)
	case TagString:
		return r.string()
	case TagList:
		it, err := r.tag()
		if err != nil {
			return nil, err
		}
		b, err := r.read(4)
		if err != nil {
			return nil, err
		}
		n := int(int32(binary.LittleEndian.Uint32(b)))
		out := make([]any, 0, max(n, 0))
		for i := 0; i < n; i++ {
			v, err := r.payload(it)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case TagCompound:
		out := map[string]any{}
		for {
			ct, err := r.tag()
			if err != nil {
				return nil, err
			}
			if ct == TagEnd {
				return out, nil
			}
			name, err := r.string()
			if err != nil {
				return nil, err
			}
			v, err := r.payload(ct)
			if err != nil {
				return nil, err
			}
			out[name] = v
		}
	case TagIntArray:
		b, err := r.read(4)
		if err != nil {
			return nil, err
		}
		n := int(int32(binary.LittleEndian.Uint32(b)))
		buf, err := r.read(4 * n)
		if err != nil {
			return nil, err
		}
		out := make([]int32, n)
		for i := range out {
			out[i] = int32(binary.LittleEndian.Uint32(buf[4*i:]))
		}
		return out, nil
	case TagLongArray:
		b, err := r.read(4)
		if err != nil {
			return nil, err
		}
		n := int(int32(binary.LittleEndian.Uint32(b)))
		buf, err := r.read(8 * n)
		if err != nil {
			return nil, err
		}
		out := make([]int64, n)
		for i := range out {
			out[i] = int64(binary.LittleEndian.Uint64(buf[8*i:]))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("未知的 NBT 标签类型 %d", t)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
