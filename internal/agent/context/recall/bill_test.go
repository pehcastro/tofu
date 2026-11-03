package recall

type Bill struct {
	CacheRead int
	Fresh     int
}

func (b Bill) Total() int {
	return b.CacheRead + b.Fresh
}
