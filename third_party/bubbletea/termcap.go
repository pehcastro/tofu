package tea

type requestCapabilityMsg string

func RequestCapability(s string) Cmd {
	return func() Msg {
		return requestCapabilityMsg(s)
	}
}

type CapabilityMsg struct {
	Content string
}

func (c CapabilityMsg) String() string {
	return c.Content
}
