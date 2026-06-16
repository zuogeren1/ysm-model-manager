package litematic

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"sort"

	"ysm-model-manager/go/types"
)

// ParseMeta 解析 .litematic 文件的元数据和方块统计
func ParseMeta(path string) (*types.LitematicMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	root, err := ReadRootCompound(gz)
	if err != nil {
		return nil, fmt.Errorf("nbt: %w", err)
	}

	meta := &types.LitematicMeta{}

	if v, ok := GetInt(root, "Version"); ok {
		meta.Version = v
	}
	if v, ok := GetInt(root, "MinecraftDataVersion"); ok {
		meta.MinecraftDataVersion = v
	}

	metadata := GetCompound(root, "Metadata")
	if metadata == nil {
		return nil, fmt.Errorf("缺少 Metadata compound")
	}

	meta.Name, _ = GetString(metadata, "Name")
	meta.Author, _ = GetString(metadata, "Author")
	meta.Description, _ = GetString(metadata, "Description")
	meta.TimeCreated, _ = GetLong(metadata, "TimeCreated")
	meta.TimeModified, _ = GetLong(metadata, "TimeModified")
	if v, ok := GetInt(metadata, "TotalBlocks"); ok {
		meta.TotalBlocks = v
	}
	if v, ok := GetInt(metadata, "TotalVolume"); ok {
		meta.TotalVolume = v
	}

	if encSize := GetCompound(metadata, "EnclosingSize"); encSize != nil {
		var size [3]int
		if v, ok := GetInt(encSize, "x"); ok {
			size[0] = v
		}
		if v, ok := GetInt(encSize, "y"); ok {
			size[1] = v
		}
		if v, ok := GetInt(encSize, "z"); ok {
			size[2] = v
		}
		meta.EnclosingSize = size
	}

	// PreviewImage: ARGB byte array → RGBA → PNG → base64
	if previewData, ok := GetByteArray(metadata, "PreviewImage"); ok && len(previewData) > 0 {
		meta.PreviewImage = convertPreviewImage(previewData)
	}

	regions := GetCompound(root, "Regions")
	if regions != nil {
		meta.RegionCount = len(regions)
		meta.BlockStats = aggregateBlockStatsFromPalette(regions)
	}

	return meta, nil
}

// aggregateBlockStatsFromPalette 从所有 region 的 palette 中按名称聚合，精确计数
// 复用 buildRegionInfo 的标准化逻辑
func aggregateBlockStatsFromPalette(regions TagCompound) []types.LitematicBlockStat {
	counts := make(map[string]int)

	for _, regionTag := range regions {
		region, ok := regionTag.(TagCompound)
		if !ok {
			continue
		}

		paletteList := GetList(region, "BlockStatePalette")
		if paletteList == nil || len(paletteList.Elements) <= 1 {
			continue
		}

		paletteNames := make([]string, len(paletteList.Elements))
		for i, elem := range paletteList.Elements {
			if nameTag := GetCompoundKey(elem, "Name"); nameTag != nil {
				if name, ok := nameTag.(TagString); ok {
					paletteNames[i] = string(name)
				}
			}
		}

		info := buildRegionInfo(region)
		if info == nil {
			continue
		}

		totalBlocks := info.sizeX * info.sizeY * info.sizeZ
		for i := 0; i < totalBlocks; i++ {
			paletteIdx := extractBits(info.longs, i*info.bpe, info.bpe)
			if paletteIdx < 0 || paletteIdx >= len(paletteNames) || paletteIdx == 0 {
				continue
			}
			if name := paletteNames[paletteIdx]; name != "" {
				counts[name]++
			}
		}
	}

	stats := make([]types.LitematicBlockStat, 0, len(counts))
	for name, count := range counts {
		stats = append(stats, types.LitematicBlockStat{Name: name, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})
	return stats
}

// convertPreviewImage 将 Java ARGB byte array 转为 PNG base64 data URI
// 输入: 140×140×4 字节序列（每个像素 4 字节 ARGB，大端序）
// 输出: "data:image/png;base64,..."
func convertPreviewImage(data []byte) string {
	const size = 140
	expectedLen := size * size * 4
	if len(data) < expectedLen {
		return ""
	}

	// ARGB → RGBA
	rgba := make([]byte, expectedLen)
	for i := 0; i < size*size; i++ {
		a := data[i*4]     // Java ARGB: byte0 = Alpha
		r := data[i*4+1]   // byte1 = Red
		g := data[i*4+2]   // byte2 = Green
		b := data[i*4+3]   // byte3 = Blue
		rgba[i*4] = r      // Go RGBA: byte0 = Red
		rgba[i*4+1] = g    // byte1 = Green
		rgba[i*4+2] = b    // byte2 = Blue
		rgba[i*4+3] = a    // byte3 = Alpha
	}

	img := &image.RGBA{
		Pix:    rgba,
		Stride: size * 4,
		Rect:   image.Rect(0, 0, size, size),
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
