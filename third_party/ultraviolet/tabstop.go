package uv

const DefaultTabInterval = 8

type TabStops struct {
	stops    []int
	interval int
	width    int
}

func NewTabStops(width, interval int) *TabStops {
	ts := new(TabStops)
	ts.interval = interval
	ts.width = width
	ts.stops = make([]int, (width+(interval-1))/interval)
	ts.init(0, width)
	return ts
}

func DefaultTabStops(cols int) *TabStops {
	return NewTabStops(cols, DefaultTabInterval)
}

func (ts *TabStops) Resize(width int) {
	if width == ts.width {
		return
	}

	if width < ts.width {
		size := (width + (ts.interval - 1)) / ts.interval
		ts.stops = ts.stops[:size]
	} else {
		size := (width - ts.width + (ts.interval - 1)) / ts.interval
		ts.stops = append(ts.stops, make([]int, size)...)
	}

	ts.init(ts.width, width)
	ts.width = width
}

func (ts *TabStops) Width() int {
	return ts.width
}

func (ts TabStops) IsStop(col int) bool {
	mask := ts.mask(col)
	i := col >> 3
	if i < 0 || i >= len(ts.stops) {
		return false
	}
	return ts.stops[i]&mask != 0
}

func (ts TabStops) Next(col int) int {
	return ts.Find(col, 1)
}

func (ts TabStops) Prev(col int) int {
	return ts.Find(col, -1)
}

func (ts TabStops) Find(col, delta int) int {
	if delta == 0 {
		return col
	}

	var prev bool
	count := delta
	if count < 0 {
		count = -count
		prev = true
	}

	for count > 0 {
		if !prev {
			if col >= ts.width-1 {
				return col
			}

			col++
		} else {
			if col < 1 {
				return col
			}

			col--
		}

		if ts.IsStop(col) {
			count--
		}
	}

	return col
}

func (ts *TabStops) Set(col int) {
	mask := ts.mask(col)
	ts.stops[col>>3] |= mask
}

func (ts *TabStops) Reset(col int) {
	mask := ts.mask(col)
	ts.stops[col>>3] &= ^mask
}

func (ts *TabStops) Clear() {
	ts.stops = make([]int, len(ts.stops))
}

func (ts *TabStops) mask(col int) int {
	return 1 << (col & (ts.interval - 1))
}

func (ts *TabStops) init(col, width int) {
	for x := col; x < width; x++ {
		if x%ts.interval == 0 {
			ts.Set(x)
		} else {
			ts.Reset(x)
		}
	}
}
