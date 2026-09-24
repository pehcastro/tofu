package main

import (
	"io/fs"

	"tofu/internal/llm/models"
	"tofu/internal/turn"
	shipped "tofu/library"
)

const proxySheet = "tools/shell/proxy.yaml"

type proxySetting struct {
	proxy   *turn.CommandProxy
	use     string
	layer   string
	refused []error
}

func loadProxySetting(root string) proxySetting {
	setting := proxySetting{use: "off", layer: "nothing declares it"}
	layers, err := models.Layers(shipped.Files(), root)
	if err != nil {
		setting.refused = append(setting.refused, err)
		return setting
	}
	for _, layer := range layers {
		if _, err := fs.Stat(layer.FS, proxySheet); err != nil {
			continue
		}
		proxy, err := turn.LoadCommandProxy(layer.FS, layer.Origin, root)
		if err != nil {
			setting.refused = append(setting.refused, err)
			continue
		}
		setting.proxy, setting.use, setting.layer = proxy, "off", layer.Name+" "+layer.Origin
		if proxy != nil {
			setting.use = "rtk"
		}
	}
	return setting
}
