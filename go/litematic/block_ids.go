package litematic

import (
	"encoding/json"
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

//go:embed blocks_1_12.json
var blocksJSON []byte

type prBlock struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Variations []struct {
		Metadata int    `json:"metadata"`
		Name     string `json:"displayName"`
	} `json:"variations"`
}

var (
	blockIDToName     map[int]string
	blockVariantNames map[string]string
	legacyBlockMapOnce sync.Once
)

func loadLegacyBlocks() {
	blockIDToName = make(map[int]string)
	blockVariantNames = make(map[string]string)

	var blocks []prBlock
	if json.Unmarshal(blocksJSON, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		blockIDToName[b.ID] = b.Name
		if len(b.Variations) == 0 {
			blockVariantNames[fmt.Sprintf("%d:0", b.ID)] = b.Name
			continue
		}
		for _, v := range b.Variations {
			key := fmt.Sprintf("%d:%d", b.ID, v.Metadata)
			if v.Name != "" {
				blockVariantNames[key] = toName(v.Name)
			} else {
				blockVariantNames[key] = b.Name
			}
		}
	}
}

func toName(display string) string {
	return strings.ToLower(strings.ReplaceAll(display, " ", "_"))
}

func ResolveBlockName(id int, data byte) string {
	legacyBlockMapOnce.Do(loadLegacyBlocks)
	if blockVariantNames == nil {
		return ""
	}
	key := fmt.Sprintf("%d:%d", id, data)
	if v, ok := blockVariantNames[key]; ok {
		return v
	}
	key0 := fmt.Sprintf("%d:0", id)
	if v, ok := blockVariantNames[key0]; ok {
		return v
	}
	return ""
}
