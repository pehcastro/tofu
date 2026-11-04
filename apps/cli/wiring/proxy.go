package wiring

import (
	"io/fs"

	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/catalog"
	shipped "tofu/library"
)

const proxySheet = "tools/shell/proxy.yaml"

type ProxySetting struct {
	Proxy   *loop.CommandProxy
	Use     string
	Layer   string
	Refused []error
}

func LoadProxySetting(root string) ProxySetting {
	setting := ProxySetting{Use: "off", Layer: "nothing declares it"}
	layers, err := catalog.Layers(shipped.Files(), root)
	if err != nil {
		setting.Refused = append(setting.Refused, err)
		return setting
	}
	for _, layer := range layers {
		if _, err := fs.Stat(layer.FS, proxySheet); err != nil {
			continue
		}
		proxy, err := loop.LoadCommandProxy(layer.FS, layer.Origin, root)
		if err != nil {
			setting.Refused = append(setting.Refused, err)
			continue
		}
		setting.Proxy, setting.Use, setting.Layer = proxy, "off", layer.Name+" "+layer.Origin
		if layer.Origin == layer.Name {
			setting.Layer = layer.Name
		}
		if proxy != nil {
			setting.Use = "rtk"
		}
	}
	return setting
}
