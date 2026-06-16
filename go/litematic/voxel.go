package litematic

import (
	"compress/gzip"
	"fmt"
	"os"

	"ysm-model-manager/go/types"
)

// regionInfo 标准化后的 region 遍历信息
type regionInfo struct {
	originX, originY, originZ int
	sizeX, sizeY, sizeZ       int
	palette                   []string // 索引→颜色字符串
	longs                     []int64
	bpe                       int
}

// BuildVoxelData 构建体素渲染数据（按颜色分组）
func BuildVoxelData(path string, maxBlocks int) (*types.LitematicVoxelData, error) {
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

	encSize := [3]int{}
	if metadata := GetCompound(root, "Metadata"); metadata != nil {
		if es := GetCompound(metadata, "EnclosingSize"); es != nil {
			if v, ok := GetInt(es, "x"); ok {
				encSize[0] = v
			}
			if v, ok := GetInt(es, "y"); ok {
				encSize[1] = v
			}
			if v, ok := GetInt(es, "z"); ok {
				encSize[2] = v
			}
		}
	}

	regions := GetCompound(root, "Regions")
	if regions == nil {
		return &types.LitematicVoxelData{Size: encSize}, nil
	}

	var regionInfos []regionInfo
	for _, regionTag := range regions {
		region, ok := regionTag.(TagCompound)
		if !ok {
			continue
		}
		info := buildRegionInfo(region)
		if info == nil {
			continue
		}
		regionInfos = append(regionInfos, *info)
	}

	colorGroups := make(map[string][][3]int16)
	blockCount := 0
	truncated := false

	for _, info := range regionInfos {
		totalInRegion := info.sizeX * info.sizeY * info.sizeZ
		for i := 0; i < totalInRegion; i++ {
			if blockCount >= maxBlocks {
				truncated = true
				break
			}
			paletteIdx := extractBits(info.longs, i*info.bpe, info.bpe)
			if paletteIdx < 0 || paletteIdx >= len(info.palette) || paletteIdx == 0 {
				continue // air or invalid
			}

			// 计算全局坐标（Minecraft 存储顺序：X→Z→Y，Y 最慢）
			// 公式: i = x + z * sizeX + y * sizeX * sizeZ
			gx := int16(info.originX + (i % info.sizeX))
			gz := int16(info.originZ + ((i / info.sizeX) % info.sizeZ))
			gy := int16(info.originY + (i / (info.sizeX * info.sizeZ)))

			color := info.palette[paletteIdx]
			colorGroups[color] = append(colorGroups[color], [3]int16{gx, gy, gz})
			blockCount++
		}
		if truncated {
			break
		}
	}

	groups := make([]types.VoxelGroup, 0, len(colorGroups))
	for color, positions := range colorGroups {
		groups = append(groups, types.VoxelGroup{
			Color:     color,
			Positions: positions,
		})
	}

	return &types.LitematicVoxelData{
		Size:      encSize,
		Groups:    groups,
		Truncated: truncated,
	}, nil
}

// buildRegionInfo 标准化一个 region 的遍历信息
func buildRegionInfo(region TagCompound) *regionInfo {
	paletteList := GetList(region, "BlockStatePalette")
	if paletteList == nil || len(paletteList.Elements) <= 1 {
		return nil
	}

	palette := make([]string, len(paletteList.Elements))
	for i, elem := range paletteList.Elements {
		nameTag := GetCompoundKey(elem, "Name")
		if name, ok := nameTag.(TagString); ok {
			palette[i] = MapColor(string(name))
		} else {
			palette[i] = "#000000"
		}
	}

	sizeCompound := GetCompound(region, "Size")
	if sizeCompound == nil {
		return nil
	}
	sx, _ := GetInt(sizeCompound, "x")
	sy, _ := GetInt(sizeCompound, "y")
	sz, _ := GetInt(sizeCompound, "z")

	posCompound := GetCompound(region, "Position")
	ox, oy, oz := 0, 0, 0
	if posCompound != nil {
		ox, _ = GetInt(posCompound, "x")
		oy, _ = GetInt(posCompound, "y")
		oz, _ = GetInt(posCompound, "z")
	}

	// 负 size 标准化
	if sx < 0 {
		ox += sx + 1
		sx = -sx
	}
	if sy < 0 {
		oy += sy + 1
		sy = -sy
	}
	if sz < 0 {
		oz += sz + 1
		sz = -sz
	}

	longs, ok := GetLongArray(region, "BlockStates")
	if !ok || len(longs) == 0 {
		return nil
	}

	bpe := bitsPerEntry(len(palette))
	if bpe == 0 {
		return nil
	}

	return &regionInfo{
		originX: ox, originY: oy, originZ: oz,
		sizeX: sx, sizeY: sy, sizeZ: sz,
		palette: palette,
		longs:   longs,
		bpe:     bpe,
	}
}
