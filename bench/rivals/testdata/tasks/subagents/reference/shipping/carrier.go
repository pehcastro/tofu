package shipping

const (
	maxGrams        = 30000
	carrierAttempts = 5
	trackingPrefix  = "DP"
)

type Carrier interface {
	Send(l Label) error
}

type RecordingCarrier struct {
	Labels []Label
}

func (c *RecordingCarrier) Send(l Label) error {
	c.Labels = append(c.Labels, l)
	return nil
}
