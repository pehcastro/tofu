package llm

type CutReply struct {
	Streamed string
	Err      error
}

func (c CutReply) Error() string { return c.Err.Error() }
func (c CutReply) Unwrap() error { return c.Err }

func CutAfter(streamed string, err error) error {
	if err == nil || streamed == "" {
		return err
	}
	return CutReply{Streamed: streamed, Err: err}
}
