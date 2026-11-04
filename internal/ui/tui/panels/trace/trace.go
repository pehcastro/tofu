package trace

import "tofu/internal/tools/registry"

func Short(id string) string { return registry.ShortID(id) }
