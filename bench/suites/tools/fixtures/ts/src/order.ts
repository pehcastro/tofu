import * as pr from "./pricing.ts";

export class Order {
  quantity: number;
  unitCents: number;

  constructor(quantity: number, unitCents: number) {
    this.quantity = quantity;
    this.unitCents = unitCents;
  }

  total(): number {
    return pr.total(this.quantity, this.unitCents);
  }

  discounted(): number {
    return this.total() - 100;
  }
}
