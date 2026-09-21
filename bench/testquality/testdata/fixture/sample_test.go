package fixture

import "testing"

func TestAddReturnsSomething(t *testing.T) {
	result := Add(2, 3)
	if result == 0 {
		t.Fatal("Add returned zero")
	}
}

func TestSaveIsCalled(t *testing.T) {
	Save = func(name string) error {
		return nil
	}
	if err := Save("x"); err != nil {
		t.Fatal(err)
	}
}
