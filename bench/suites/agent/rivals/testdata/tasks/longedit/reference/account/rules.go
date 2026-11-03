package account

const overdraftCents = 5000

func CanWithdraw(a Account, cents int64) bool {
	return cents > 0 && a.Balance-cents >= -overdraftCents
}

func Overdrawn(accounts []Account) []Account {
	var over []Account
	for _, a := range accounts {
		if a.Balance < 0 {
			over = append(over, a)
		}
	}
	return over
}
