function checksum(items) {
  let total = 0;
  for (let round = 0; round < 3_000_000; round++) {
    for (const item of items) {
      total = (total * 31 + item.length + round) % 1_000_003;
    }
  }
  return total;
}

console.log(checksum(["tea", "rice", "tofu", "miso"]));
