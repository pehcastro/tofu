def checksum(items):
    total = 0
    for round in range(200_000):
        for item in items:
            total = (total * 31 + len(item) + round) % 1_000_003
    return total


print(checksum(["tea", "rice", "tofu", "miso"]))
