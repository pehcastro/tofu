package shop

func ApplyDiscount(total, percent int) int {
	if percent <= 0 {
		return total
	}
	if percent >= 100 {
		return 0
	}
	return total - total*percent/100
}

func (c Cart) DiscountedTotal(percent int) int {
	return ApplyDiscount(c.Subtotal(), percent)
}
