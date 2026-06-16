// Package litematic Litematica 投影文件 (.litematic) 的解析和预览数据构建。
// NBT 二进制格式的读取，参考 https://wiki.vg/NBT。
package litematic

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	tagByte       = 1
	tagShort      = 2
	tagInt        = 3
	tagLong       = 4
	tagFloat      = 5
	tagDouble     = 6
	tagByteArray  = 7
	tagString     = 8
	tagList       = 9
	tagCompound   = 10
	tagIntArray   = 11
	tagLongArray  = 12
)

type NbtTag interface {
	Type() byte
}

type TagByte byte
func (t TagByte) Type() byte { return tagByte }

type TagShort int16
func (t TagShort) Type() byte { return tagShort }

type TagInt int32
func (t TagInt) Type() byte { return tagInt }

type TagLong int64
func (t TagLong) Type() byte { return tagLong }

type TagFloat float32
func (t TagFloat) Type() byte { return tagFloat }

type TagDouble float64
func (t TagDouble) Type() byte { return tagDouble }

type TagString string
func (t TagString) Type() byte { return tagString }

type TagByteArray []byte
func (t TagByteArray) Type() byte { return tagByteArray }

type TagIntArray []int32
func (t TagIntArray) Type() byte { return tagIntArray }

type TagLongArray []int64
func (t TagLongArray) Type() byte { return tagLongArray }

type TagList struct {
	ElemType byte
	Elements []NbtTag
}
func (t *TagList) Type() byte { return tagList }

type TagCompound map[string]NbtTag
func (t TagCompound) Type() byte { return tagCompound }

func readByte(r io.Reader) (byte, error) {
	var buf [1]byte
	_, err := io.ReadFull(r, buf[:])
	return buf[0], err
}

func readInt16(r io.Reader) (int16, error) {
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(buf[:])), nil
}

func readInt32(r io.Reader) (int32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(buf[:])), nil
}

func readInt64(r io.Reader) (int64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(buf[:])), nil
}

func readBytes(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func readString(r io.Reader) (string, error) {
	length, err := readInt16(r)
	if err != nil {
		return "", err
	}
	if length < 0 {
		return "", fmt.Errorf("negative string length: %d", length)
	}
	data, err := readBytes(r, int(length))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readTagName(r io.Reader) (string, error) {
	return readString(r)
}

func ReadTag(r io.Reader) (string, NbtTag, error) {
	typeID, err := readByte(r)
	if err != nil {
		return "", nil, err
	}
	if typeID == 0 {
		return "", nil, nil
	}
	name, err := readTagName(r)
	if err != nil {
		return "", nil, err
	}
	tag, err := readPayload(r, typeID)
	if err != nil {
		return "", nil, err
	}
	return name, tag, nil
}

func ReadTagUnnamed(r io.Reader, typeID byte) (NbtTag, error) {
	return readPayload(r, typeID)
}

func readPayload(r io.Reader, typeID byte) (NbtTag, error) {
	switch typeID {
	case tagByte:
		b, err := readByte(r)
		return TagByte(b), err
	case tagShort:
		v, err := readInt16(r)
		return TagShort(v), err
	case tagInt:
		v, err := readInt32(r)
		return TagInt(v), err
	case tagLong:
		v, err := readInt64(r)
		return TagLong(v), err
	case tagFloat:
		v, err := readInt32(r)
		return TagFloat(math.Float32frombits(uint32(v))), err
	case tagDouble:
		v, err := readInt64(r)
		return TagDouble(math.Float64frombits(uint64(v))), err
	case tagString:
		s, err := readString(r)
		return TagString(s), err
	case tagByteArray:
		length, err := readInt32(r)
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, fmt.Errorf("negative byte array length: %d", length)
		}
		data, err := readBytes(r, int(length))
		return TagByteArray(data), err
	case tagIntArray:
		length, err := readInt32(r)
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, fmt.Errorf("negative int array length: %d", length)
		}
		arr := make([]int32, length)
		for i := int32(0); i < length; i++ {
			v, err := readInt32(r)
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return TagIntArray(arr), nil
	case tagLongArray:
		length, err := readInt32(r)
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, fmt.Errorf("negative long array length: %d", length)
		}
		arr := make([]int64, length)
		for i := int32(0); i < length; i++ {
			v, err := readInt64(r)
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return TagLongArray(arr), nil
	case tagList:
		elemType, err := readByte(r)
		if err != nil {
			return nil, err
		}
		count, err := readInt32(r)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, fmt.Errorf("negative list count: %d", count)
		}
		elements := make([]NbtTag, 0, count)
		for i := int32(0); i < count; i++ {
			elem, err := ReadTagUnnamed(r, elemType)
			if err != nil {
				return nil, fmt.Errorf("list element %d: %w", i, err)
			}
			elements = append(elements, elem)
		}
		return &TagList{ElemType: elemType, Elements: elements}, nil
	case tagCompound:
		compound := make(TagCompound)
		for {
			name, tag, err := ReadTag(r)
			if err != nil {
				return nil, err
			}
			if tag == nil {
				break
			}
			compound[name] = tag
		}
		return compound, nil
	default:
		return nil, fmt.Errorf("unsupported NBT tag type: %d", typeID)
	}
}

func ReadRootCompound(r io.Reader) (TagCompound, error) {
	typeID, err := readByte(r)
	if err != nil {
		return nil, fmt.Errorf("read root type: %w", err)
	}
	if typeID != tagCompound {
		return nil, fmt.Errorf("root tag is not TAG_Compound, got %d", typeID)
	}
	_, err = readTagName(r)
	if err != nil {
		return nil, fmt.Errorf("read root name: %w", err)
	}
	compound := make(TagCompound)
	for {
		name, tag, err := ReadTag(r)
		if err != nil {
			return nil, err
		}
		if tag == nil {
			break
		}
		compound[name] = tag
	}
	return compound, nil
}

func GetCompound(tag NbtTag, key string) TagCompound {
	if compound, ok := tag.(TagCompound); ok {
		if child := compound[key]; child != nil {
			if c, ok := child.(TagCompound); ok {
				return c
			}
		}
	}
	return nil
}

func GetInt(compound TagCompound, key string) (int, bool) {
	if child := compound[key]; child != nil {
		if v, ok := child.(TagInt); ok {
			return int(v), true
		}
	}
	return 0, false
}

func GetLong(compound TagCompound, key string) (int64, bool) {
	if child := compound[key]; child != nil {
		if v, ok := child.(TagLong); ok {
			return int64(v), true
		}
	}
	return 0, false
}

func GetString(compound TagCompound, key string) (string, bool) {
	if child := compound[key]; child != nil {
		if v, ok := child.(TagString); ok {
			return string(v), true
		}
	}
	return "", false
}

func GetByteArray(compound TagCompound, key string) ([]byte, bool) {
	if child := compound[key]; child != nil {
		if v, ok := child.(TagByteArray); ok {
			return []byte(v), true
		}
	}
	return nil, false
}

func GetLongArray(compound TagCompound, key string) ([]int64, bool) {
	if child := compound[key]; child != nil {
		if v, ok := child.(TagLongArray); ok {
			return []int64(v), true
		}
	}
	return nil, false
}

func GetList(compound TagCompound, key string) *TagList {
	if child := compound[key]; child != nil {
		if v, ok := child.(*TagList); ok {
			return v
		}
	}
	return nil
}

func GetCompoundKey(tag NbtTag, key string) NbtTag {
	if compound, ok := tag.(TagCompound); ok {
		return compound[key]
	}
	return nil
}

// Litematica 使用小端位序将方块索引打包到 LongArray：
// 索引从每个 long 的 LSB 开始连续排列，可跨越 64 位边界。
// 这与 Minecraft 1.16+ 原版 packed array 的大端位序不同——搞反会导致 3D 预览全乱。
func extractBits(longs []int64, bitOffset, bitCount int) int {
	if bitCount == 0 {
		return 0
	}
	longIdx := bitOffset / 64
	bitPos := bitOffset % 64
	mask := (uint64(1) << bitCount) - 1

	if bitPos+bitCount <= 64 {
		return int((uint64(longs[longIdx]) >> bitPos) & mask)
	}

	bitsFromFirst := 64 - bitPos
	bitsFromSecond := bitCount - bitsFromFirst
	low := uint64(longs[longIdx]) >> bitPos
	high := uint64(longs[longIdx+1]) & ((uint64(1) << bitsFromSecond) - 1)
	return int(low | (high << bitsFromFirst))
}

func bitsPerEntry(paletteSize int) int {
	if paletteSize <= 1 {
		return 0
	}
	b := int(math.Ceil(math.Log2(float64(paletteSize))))
	if b < 2 {
		b = 2
	}
	return b
}
