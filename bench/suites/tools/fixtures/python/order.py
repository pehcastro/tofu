from dataclasses import dataclass

import pricing as pr


@dataclass
class Order:
    quantity: int
    unit_cents: int

    def total(self) -> int:
        return pr.total(self.quantity, self.unit_cents)

    def discounted(self) -> int:
        return self.total() - 100
