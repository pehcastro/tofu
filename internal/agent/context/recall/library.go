package recall

import (
	"embed"
	"fmt"
	"strconv"
	"strings"

	"tofu/internal/konst"
)

//go:embed data
var library embed.FS

type Config struct {
	ElideAboveBytes          int
	HeadBytes                int
	TailBytes                int
	BytesPerThousandTokens   int
	BytesPerThousandTokensOn map[string]int
	CompactFloorBytes        int
}

const (
	conservativeBytesPerThousandTokens = 2000
	conservativeCompactFloorBytes      = 256
	wireRatioPrefix                    = "bytes_per_thousand_tokens_"
)

func (c Config) OnWire(wire string) Config {
	if ratio, known := c.BytesPerThousandTokensOn[wire]; known {
		c.BytesPerThousandTokens = ratio
	}
	return c
}

func (c Config) Tokens(text string) int {
	return len(text) * 1000 / c.BytesPerThousandTokens
}

func (c Config) MessageTokens(text string) int {
	return c.Tokens(text) + konst.MessageFramingTokens
}

func LoadConfig() (Config, error) {
	data, err := library.ReadFile("data/elide.yaml")
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(data)
}

func numbersByName(data []byte) (map[string]int, error) {
	fields := make(map[string]int)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("recall: library line %q has no key", raw)
		}
		key = strings.TrimSpace(key)
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("recall: library %q: %w", key, err)
		}
		fields[key] = n
	}
	return fields, nil
}

func ParseConfig(data []byte) (Config, error) {
	fields, err := numbersByName(data)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		ElideAboveBytes:          fields["elide_above_bytes"],
		HeadBytes:                fields["head_bytes"],
		TailBytes:                fields["tail_bytes"],
		BytesPerThousandTokens:   fields["bytes_per_thousand_tokens"],
		BytesPerThousandTokensOn: map[string]int{},
		CompactFloorBytes:        fields["compact_floor_bytes"],
	}
	for key, ratio := range fields {
		if wire, found := strings.CutPrefix(key, wireRatioPrefix); found && ratio > 0 {
			cfg.BytesPerThousandTokensOn[wire] = ratio
		}
	}
	if cfg.ElideAboveBytes <= 0 {
		return Config{}, fmt.Errorf("recall: elide library needs a positive elide_above_bytes")
	}
	if cfg.BytesPerThousandTokens <= 0 {
		cfg.BytesPerThousandTokens = conservativeBytesPerThousandTokens
	}
	if cfg.CompactFloorBytes <= 0 {
		cfg.CompactFloorBytes = conservativeCompactFloorBytes
	}
	return cfg, nil
}
