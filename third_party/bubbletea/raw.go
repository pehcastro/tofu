package tea

type RawMsg struct {
	Msg any
}

func Raw(r any) Cmd {
	return func() Msg {
		return RawMsg{r}
	}
}
