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
	"strings"

	"ysm-model-manager/go/types"
)

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
		cn := ResolveBlockZH(strings.TrimPrefix(name, "minecraft:"))
		stats = append(stats, types.LitematicBlockStat{Name: cn, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})
	return stats
}

func convertPreviewImage(data []byte) string {
	const size = 140
	expectedLen := size * size * 4
	if len(data) < expectedLen {
		return ""
	}

	rgba := make([]byte, expectedLen)
	for i := 0; i < size*size; i++ {
		a := data[i*4]
		r := data[i*4+1]
		g := data[i*4+2]
		b := data[i*4+3]
		rgba[i*4] = r
		rgba[i*4+1] = g
		rgba[i*4+2] = b
		rgba[i*4+3] = a
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

func ParseSchematic(path string) map[string]interface{} {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	defer gz.Close()

	root, err := ReadRootCompound(gz)
	if err != nil {
		return nil
	}

	result := map[string]interface{}{}

	if v, ok := GetInt(root, "Version"); ok {
		result["version"] = v
	}
	if v, ok := GetInt(root, "DataVersion"); ok {
		result["dataVersion"] = v
	}

	w, wok := GetInt(root, "Width")
	h, hok := GetInt(root, "Height")
	l, lok := GetInt(root, "Length")
	if wok && hok && lok {
		result["size"] = []int{w, h, l}
	}

	metaCompound := GetCompound(root, "Metadata")
	if metaCompound != nil {
		if author, ok := GetString(metaCompound, "Author"); ok {
			result["author"] = author
		}
		if name, ok := GetString(metaCompound, "Name"); ok {
			result["name"] = name
		}
	}

	blocksBA, _ := GetByteArray(root, "Blocks")
	if blocksBA != nil {
		result["blockCount"] = len(blocksBA)
	}

	paletteCompound := GetCompound(root, "Palette")
	if paletteMax, ok := GetInt(root, "PaletteMax"); ok {
		result["paletteMax"] = paletteMax
	}
	if paletteCompound != nil {
		result["paletteSize"] = len(paletteCompound)
	}

	if paletteCompound == nil && blocksBA != nil {
		dataBA, _ := GetByteArray(root, "Data")
		idCounts := map[string]int{}
		for i, id := range blocksBA {
			if id == 0 {
				continue
			}
			var d byte
			if dataBA != nil && i < len(dataBA) {
				d = dataBA[i]
			}
			name := ResolveBlockName(int(id), d)
			if name == "" {
				if d != 0 {
					name = fmt.Sprintf("ID:%d:%d", id, d)
				} else {
					name = fmt.Sprintf("ID:%d", id)
				}
			} else {
				name = ResolveBlockZH(name)
			}
			idCounts[name]++
		}
		stats := make([]types.LitematicBlockStat, 0, len(idCounts))
		for name, count := range idCounts {
			stats = append(stats, types.LitematicBlockStat{Name: name, Count: count})
		}
		sort.Slice(stats, func(i, j int) bool { return stats[i].Count > stats[j].Count })
		result["paletteStats"] = stats
		if m, ok := GetString(root, "Materials"); ok {
			result["materials"] = m
		}
	}

	tileEntities := GetList(root, "TileEntities")
	if tileEntities != nil {
		result["tileEntityCount"] = len(tileEntities.Elements)
	}
	entities := GetList(root, "Entities")
	if entities != nil {
		result["entityCount"] = len(entities.Elements)
	}

	if len(result) <= 1 {
		return nil
	}
	return result
}

func ParseNbtStructure(path string) map[string]interface{} {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	defer gz.Close()

	root, err := ReadRootCompound(gz)
	if err != nil {
		return nil
	}

	sizeList := GetList(root, "size")
	blocksList := GetList(root, "blocks")
	paletteList := GetList(root, "palette")
	entitiesList := GetList(root, "entities")
	if sizeList == nil && blocksList == nil && paletteList == nil {
		return nil
	}

	result := map[string]interface{}{}
	if v, ok := GetInt(root, "DataVersion"); ok {
		result["dataVersion"] = v
	}
	if sizeList != nil && len(sizeList.Elements) == 3 {
		sx, _ := sizeList.Elements[0].(TagInt)
		sy, _ := sizeList.Elements[1].(TagInt)
		sz, _ := sizeList.Elements[2].(TagInt)
		result["size"] = []int{int(sx), int(sy), int(sz)}
	}
	if blocksList != nil {
		result["blockCount"] = len(blocksList.Elements)
	}
	if entitiesList != nil {
		result["entityCount"] = len(entitiesList.Elements)
	}
	if paletteList != nil {
		counts := map[string]int{}
		for _, elem := range paletteList.Elements {
			nameTag := GetCompoundKey(elem, "Name")
			if name, ok := nameTag.(TagString); ok && string(name) != "" {
				cn := ResolveBlockZH(strings.TrimPrefix(string(name), "minecraft:"))
				counts[cn]++
			}
		}
		stats := make([]types.LitematicBlockStat, 0, len(counts))
		for name, count := range counts {
			stats = append(stats, types.LitematicBlockStat{Name: name, Count: count})
		}
		sort.Slice(stats, func(i, j int) bool { return stats[i].Count > stats[j].Count })
		if len(stats) > 0 {
			result["paletteStats"] = stats
		}
	}
	return result
}
