module CounterBroken

let advance (limit:pos) (n:nat{n < limit})
  : Tot (r:nat{r == n + 1 /\ r <= limit}) =
  n + 2
