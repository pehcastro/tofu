import unittest

from order import Order


class OrderTest(unittest.TestCase):
    def test_total(self):
        got = Order(quantity=3, unit_cents=250).total()
        want = 750
        self.assertEqual(got, want, f"got {got}, want {want}")

    def test_discounted(self):
        got = Order(quantity=3, unit_cents=250).discounted()
        want = 675
        self.assertEqual(got, want, f"got {got}, want {want}")

    @unittest.skip("shipping rates are not modelled yet")
    def test_shipping(self):
        pass
