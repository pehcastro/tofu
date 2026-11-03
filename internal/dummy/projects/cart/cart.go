package shop

type Item struct {
	SKU   string
	Price int
	Qty   int
}

type Cart struct {
	Items []Item
}

func (c *Cart) Add(item Item) {
	for i, existing := range c.Items {
		if existing.SKU == item.SKU {
			c.Items[i].Qty += item.Qty
			return
		}
	}
	c.Items = append(c.Items, item)
}

func (c Cart) CalcTotal() int {
	total := 0
	for _, item := range c.Items {
		total += item.Price * item.Qty
	}
	return total
}
