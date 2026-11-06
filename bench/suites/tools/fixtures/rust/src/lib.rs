mod pricing;

use crate::pricing as pr;

pub struct Order {
    pub quantity: u32,
    pub unit_cents: u32,
}

impl Order {
    pub fn total(&self) -> u32 {
        pr::total(self.quantity, self.unit_cents)
    }

    pub fn discounted(&self) -> u32 {
        self.total() - 100
    }
}

#[cfg(test)]
mod tests {
    use super::Order;

    #[test]
    fn total() {
        let got = Order { quantity: 3, unit_cents: 250 }.total();
        let want = 750;
        assert_eq!(got, want, "got {got}, want {want}");
    }

    #[test]
    fn discounted() {
        let got = Order { quantity: 3, unit_cents: 250 }.discounted();
        let want = 675;
        assert_eq!(got, want, "got {got}, want {want}");
    }

    #[test]
    #[ignore = "shipping rates are not modelled yet"]
    fn shipping() {}
}
