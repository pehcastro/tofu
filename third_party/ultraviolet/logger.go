package uv

type Logger interface {
	Printf(format string, v ...interface{})
}
