package litematic

import (
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"github.com/Tnze/go-mc/nbt"
)

// buildTestLitematic 用 go-mc/nbt 编码一个最小 litematic 文件（1 个 stone 方块），
// 返回 gzip 压缩后的临时文件路径。
func buildTestLitematic(t *testing.T) string {
	t.Helper()

	root := map[string]any{
		"Version":              int32(6),
		"MinecraftDataVersion": int32(2730),
		"Metadata": map[string]any{
			"Name":        "test_projection",
			"Author":      "tester",
			"Description": "minimal test fixture",
			"TimeCreated": int64(1718500000000),
			"TimeModified": int64(1718600000000),
			"TotalBlocks": int32(1),
			"TotalVolume": int32(1),
			"EnclosingSize": map[string]any{
				"x": int32(1),
				"y": int32(1),
				"z": int32(1),
			},
		},
		"Regions": map[string]any{
			"test_region": map[string]any{
				"Position": map[string]any{
					"x": int32(0),
					"y": int32(0),
					"z": int32(0),
				},
				"Size": map[string]any{
					"x": int32(1),
					"y": int32(1),
					"z": int32(1),
				},
				"BlockStatePalette": []any{
					map[string]any{"Name": "minecraft:air"},
					map[string]any{"Name": "minecraft:stone"},
				},
				"BlockStates": []int64{1}, // 小端位序: bit0=1 → 索引 1 = stone
			},
		},
	}

	f, err := os.CreateTemp("", "test-*.litematic")
	if err != nil {
		t.Fatal(err)
	}

	gw := gzip.NewWriter(f)
	if err := nbt.NewEncoder(gw).Encode(root, ""); err != nil {
		gw.Close()
		f.Close()
		t.Fatal("encode:", err)
	}
	gw.Close()
	f.Close()

	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestParseMeta(t *testing.T) {
	path := buildTestLitematic(t)

	meta, err := ParseMeta(path)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	if meta.Name != "test_projection" {
		t.Errorf("Name = %q, want %q", meta.Name, "test_projection")
	}
	if meta.Author != "tester" {
		t.Errorf("Author = %q, want %q", meta.Author, "tester")
	}
	if meta.Description != "minimal test fixture" {
		t.Errorf("Description = %q", meta.Description)
	}
	if meta.Version != 6 {
		t.Errorf("Version = %d, want 6", meta.Version)
	}
	if meta.TotalBlocks != 1 {
		t.Errorf("TotalBlocks = %d, want 1", meta.TotalBlocks)
	}
	if meta.EnclosingSize != [3]int{1, 1, 1} {
		t.Errorf("EnclosingSize = %v, want [1 1 1]", meta.EnclosingSize)
	}
	if meta.RegionCount != 1 {
		t.Errorf("RegionCount = %d, want 1", meta.RegionCount)
	}
	if len(meta.BlockStats) == 0 {
		t.Error("BlockStats should not be empty")
	}
	// 方块统计应按数量降序——只有 1 个 stone
	found := false
	for _, s := range meta.BlockStats {
		if strings.Contains(s.Name, "石头") || strings.Contains(s.Name, "Stone") {
			found = true
		}
		// 中文映射: stone → 石头
	}
	if !found && len(meta.BlockStats) > 0 {
		t.Logf("block stats (may be untranslated): %+v", meta.BlockStats)
	}
}

func TestBuildVoxelData(t *testing.T) {
	path := buildTestLitematic(t)

	data, err := BuildVoxelData(path, 100000)
	if err != nil {
		t.Fatalf("BuildVoxelData: %v", err)
	}
	if data.Size != [3]int{1, 1, 1} {
		t.Errorf("Size = %v, want [1 1 1]", data.Size)
	}
	if len(data.Groups) == 0 {
		t.Fatal("Groups should not be empty")
	}
	// 应该只有 1 个 stone (位置 0,0,0)
	total := 0
	for _, g := range data.Groups {
		total += len(g.Positions)
	}
	if total != 1 {
		t.Errorf("total voxels = %d, want 1", total)
	}
}

func TestBuildVoxelDataTruncated(t *testing.T) {
	path := buildTestLitematic(t)

	data, err := BuildVoxelData(path, 0) // maxBlocks=0 → 全部跳过
	if err != nil {
		t.Fatalf("BuildVoxelData: %v", err)
	}
	if !data.Truncated {
		t.Error("Truncated should be true when maxBlocks=0")
	}
}

func TestExtractBits(t *testing.T) {
	cases := []struct {
		longs     []int64
		offset    int
		count     int
		want      int
	}{
		{[]int64{0b1011}, 0, 4, 0b1011},
		{[]int64{0b1011}, 0, 2, 0b11},
		{[]int64{0b1011}, 2, 2, 0b10},
		// 位序: LSB 在前，所以 long[0]=1 的第 0 位是 1
		{[]int64{1}, 0, 1, 1},
		{[]int64{2}, 1, 1, 1},
		{[]int64{0}, 0, 1, 0},
		// 跨 64 位边界
		{[]int64{0, 1}, 63, 2, 2}, // bit 63 = 0, bit 64 (long[1] bit 0) = 1 → 10b = 2
		{[]int64{0, 0xF}, 60, 8, 0xF0},
	}

	for _, c := range cases {
		got := extractBits(c.longs, c.offset, c.count)
		if got != c.want {
			t.Errorf("extractBits(v=%v, off=%d, cnt=%d) = %d, want %d",
				c.longs, c.offset, c.count, got, c.want)
		}
	}
}

func TestBitsPerEntry(t *testing.T) {
	cases := []struct {
		paletteSize int
		want        int
	}{
		{0, 0},
		{1, 0},
		{2, 2},  // ceil(log2(2)) = 1, min 2
		{3, 2},  // ceil(log2(3)) ≈ 2, = 2
		{4, 2},
		{5, 3},  // ceil(log2(5)) ≈ 3
		{255, 8},
		{256, 8},
		{257, 9},
	}

	for _, c := range cases {
		got := bitsPerEntry(c.paletteSize)
		if got != c.want {
			t.Errorf("bitsPerEntry(%d) = %d, want %d", c.paletteSize, got, c.want)
		}
	}
}

func TestMapColor(t *testing.T) {
	// 已知方块
	if c := MapColor("minecraft:stone"); c != "#7F7F7F" {
		t.Errorf("stone color = %q, want #7F7F7F", c)
	}
	if c := MapColor("minecraft:dirt"); c != "#9B6B3D" {
		t.Errorf("dirt color = %q, want #9B6B3D", c)
	}
	// 变体后缀匹配
	if c := MapColor("minecraft:stone_stairs"); c != "#7F7F7F" {
		t.Errorf("stone_stairs color = %q, want #7F7F7F", c)
	}
	// 未知方块应有颜色（hash fallback）
	if c := MapColor("minecraft:unknown_block_xyz"); c == "" {
		t.Error("unknown block should get hash fallback color")
	}
}

func TestResolveBlockName(t *testing.T) {
	// 空气
	if n := ResolveBlockName(0, 0); n != "minecraft:air" {
		t.Errorf("block 0:0 = %q, want minecraft:air", n)
	}
	// 未知 ID
	if n := ResolveBlockName(9999, 0); n != "" {
		t.Errorf("unknown block = %q, want empty", n)
	}
}

func TestResolveBlockZH(t *testing.T) {
	if zh := ResolveBlockZH("stone"); zh == "stone" {
		t.Error("stone should have Chinese name")
	}
	if zh := ResolveBlockZH("nonexistent_block_xyz"); zh != "nonexistent_block_xyz" {
		t.Error("unknown block should return original name")
	}
	// 带 minecraft: 前缀
	if zh := ResolveBlockZH("minecraft:stone"); zh == "minecraft:stone" {
		t.Error("minecraft:stone should resolve to Chinese")
	}
}

func TestHelperFunctions(t *testing.T) {
	m := map[string]any{
		"str":    "hello",
		"int32":  int32(42),
		"int16":  int16(16),
		"bytes":  []byte{0x01, 0x02},
		"longs":  []int64{1, 2, 3},
		"list":   []any{map[string]any{"k": "v"}},
		"sub":    map[string]any{"key": "val"},
	}

	if v, ok := getString(m, "str"); !ok || v != "hello" {
		t.Errorf("getString: %q, %v", v, ok)
	}
	if v, ok := getInt(m, "int32"); !ok || v != 42 {
		t.Errorf("getInt int32: %d, %v", v, ok)
	}
	if v, ok := getInt(m, "int16"); !ok || v != 16 {
		t.Errorf("getInt int16: %d, %v", v, ok)
	}
	if v, ok := getInt(m, "nope"); ok || v != 0 {
		t.Errorf("getInt missing: %d, %v", v, ok)
	}
	if v, ok := getByteArray(m, "bytes"); !ok || len(v) != 2 {
		t.Errorf("getByteArray: %v, %v", v, ok)
	}
	if v, ok := getLongArray(m, "longs"); !ok || len(v) != 3 {
		t.Errorf("getLongArray: %v, %v", v, ok)
	}
	if l := getList(m, "list"); l == nil || len(l) != 1 {
		t.Errorf("getList: %v", l)
	}
	if s := getCompound(m, "sub"); s == nil || s["key"] != "val" {
		t.Errorf("getCompound: %v", s)
	}
	if v := getCompoundKey(m, "str"); v != "hello" {
		t.Errorf("getCompoundKey: %v", v)
	}
}
