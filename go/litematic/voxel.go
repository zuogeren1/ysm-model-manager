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

func BuildNbtVoxelData(path string, maxBlocks int) (*types.LitematicVoxelData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	root, err := ReadRootCompound(gz)
	if err != nil {
		return nil, err
	}

	sizeList := GetList(root, "size")
	blocksList := GetList(root, "blocks")
	paletteList := GetList(root, "palette")
	if sizeList == nil || blocksList == nil || paletteList == nil {
		return nil, fmt.Errorf("not a structure NBT file")
	}
	if len(sizeList.Elements) != 3 {
		return nil, fmt.Errorf("invalid size")
	}

	sx := int(sizeList.Elements[0].(TagInt))
	sy := int(sizeList.Elements[1].(TagInt))
	sz := int(sizeList.Elements[2].(TagInt))

	paletteColors := make([]string, len(paletteList.Elements))
	for i, elem := range paletteList.Elements {
		nameTag := GetCompoundKey(elem, "Name")
		if name, ok := nameTag.(TagString); ok {
			paletteColors[i] = MapColor(string(name))
		} else {
			paletteColors[i] = "#7F7F7F"
		}
	}

	colorGroups := make(map[string][][3]int16)
	blockCount := 0
	truncated := false
	for _, elem := range blocksList.Elements {
		if blockCount >= maxBlocks {
			truncated = true
			break
		}
		block, ok := elem.(TagCompound)
		if !ok {
			continue
		}
		posList := GetList(block, "pos")
		stateTag := block["state"]
		if posList == nil || stateTag == nil || len(posList.Elements) != 3 {
			continue
		}
		state, ok := stateTag.(TagInt)
		if !ok || int(state) < 0 || int(state) >= len(paletteColors) {
			continue
		}
		bx := int16(posList.Elements[0].(TagInt))
		by := int16(posList.Elements[1].(TagInt))
		bz := int16(posList.Elements[2].(TagInt))
		color := paletteColors[state]
		colorGroups[color] = append(colorGroups[color], [3]int16{bx, by, bz})
		blockCount++
	}

	groups := make([]types.VoxelGroup, 0, len(colorGroups))
	for color, positions := range colorGroups {
		groups = append(groups, types.VoxelGroup{Color: color, Positions: positions})
	}
	return &types.LitematicVoxelData{
		Size:      [3]int{sx, sy, sz},
		Groups:    groups,
		Truncated: truncated,
	}, nil
}

func BuildSchematicVoxelData(path string, maxBlocks int) (*types.LitematicVoxelData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	root, err := ReadRootCompound(gz)
	if err != nil {
		return nil, err
	}

	w, wok := GetInt(root, "Width")
	h, hok := GetInt(root, "Height")
	l, lok := GetInt(root, "Length")
	if !wok || !hok || !lok {
		return nil, fmt.Errorf("not a schematic file")
	}

	blocksBA, _ := GetByteArray(root, "Blocks")
	blockDataBA, _ := GetByteArray(root, "BlockData")
	dataBA, _ := GetByteArray(root, "Data")

	paletteCompound := GetCompound(root, "Palette")
	var paletteMap map[int]string
	if paletteCompound != nil {
		paletteMap = make(map[int]string)
		for name, tag := range paletteCompound {
			if id, ok := tag.(TagInt); ok {
				paletteMap[int(id)] = MapColor(name)
			}
		}
	}

	colorGroups := make(map[string][][3]int16)
	blockCount := 0
	truncated := false
	total := w * h * l

	// v1: raw Blocks byte array; v2+: varint BlockData
	if blockDataBA != nil && paletteMap != nil {
		offset := 0
		for i := 0; i < total; i++ {
			if blockCount >= maxBlocks { truncated = true; break }
			if offset >= len(blockDataBA) { break }
			blockID, newOff := readVarInt(blockDataBA, offset)
			offset = newOff
			if blockID == 0 { continue }
			color := "#7F7F7F"
			if c, ok := paletteMap[blockID]; ok { color = c }
			x := int16(i % w)
			z := int16((i / w) % l)
			y := int16(i / (w * l))
			colorGroups[color] = append(colorGroups[color], [3]int16{x, y, z})
			blockCount++
		}
	} else if blocksBA != nil {
		for i := 0; i < total && i < len(blocksBA); i++ {
			if blockCount >= maxBlocks { truncated = true; break }
			blockID := int(blocksBA[i])
			if blockID == 0 { continue }
			color := "#7F7F7F"
			if paletteMap != nil {
				if c, ok := paletteMap[blockID]; ok { color = c }
			} else if dataBA != nil && i < len(dataBA) && dataBA[i] != 0 {
				color = MapColor(fmt.Sprintf("legacy:%d:%d", blockID, dataBA[i]))
			}
			x := int16(i % w)
			z := int16((i / w) % l)
			y := int16(i / (w * l))
			colorGroups[color] = append(colorGroups[color], [3]int16{x, y, z})
			blockCount++
		}
	} else {
		return nil, fmt.Errorf("schematic has no Blocks or BlockData")
	}

	groups := make([]types.VoxelGroup, 0, len(colorGroups))
	for color, positions := range colorGroups {
		groups = append(groups, types.VoxelGroup{Color: color, Positions: positions})
	}
	return &types.LitematicVoxelData{
		Size:      [3]int{w, h, l},
		Groups:    groups,
		Truncated: truncated,
	}, nil
}

func readVarInt(data []byte, offset int) (int, int) {
	result := 0
	shift := 0
	for offset < len(data) {
		b := int(data[offset])
		offset++
		result |= (b & 0x7F) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
	}
	return result, offset
}
