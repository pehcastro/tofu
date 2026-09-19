package recall

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
)

//go:embed data/elide.yaml
var elideCatalog embed.FS

type Config struct {
	ElideAboveBytes        int
	HeadBytes              int
	TailBytes              int
	BytesPerThousandTokens int
	CompactFloorBytes      int
}

const (
	conservativeBytesPerThousandTokens = 2000
	conservativeCompactFloorBytes      = 256
)

func (c Config) Tokens(text string) int {
	return len(text) * 1000 / c.BytesPerThousandTokens
}

func LoadConfig() (Config, error) {
	data, err := elideCatalog.ReadFile("data/elide.yaml")
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	fields := make(map[string]int)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Config{}, fmt.Errorf("recall: elide catalog line %q has no key", raw)
		}
		key = strings.TrimSpace(key)
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("recall: elide catalog %q: %w", key, err)
		}
		fields[key] = n
	}
	cfg := Config{
		ElideAboveBytes:        fields["elide_above_bytes"],
		HeadBytes:              fields["head_bytes"],
		TailBytes:              fields["tail_bytes"],
		BytesPerThousandTokens: fields["bytes_per_thousand_tokens"],
		CompactFloorBytes:      fields["compact_floor_bytes"],
	}
	if cfg.ElideAboveBytes <= 0 {
		return Config{}, fmt.Errorf("recall: elide catalog needs a positive elide_above_bytes")
	}
	if cfg.BytesPerThousandTokens <= 0 {
		cfg.BytesPerThousandTokens = conservativeBytesPerThousandTokens
	}
	if cfg.CompactFloorBytes <= 0 {
		cfg.CompactFloorBytes = conservativeCompactFloorBytes
	}
	return cfg, nil
}
