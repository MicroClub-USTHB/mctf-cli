#!/usr/bin/env python3
"""Reference solver for rsa-baby. Run after factoring n."""
from sympy import factorint, mod_inverse

n = 0xC9C2A8E1B7A0E6C5
e = 65537
c = 0x123456789ABCDEF0

(p, q), _ = list(factorint(n).items())
phi = (p - 1) * (q - 1)
d = mod_inverse(e, phi)
m = pow(c, d, n)
print(m.to_bytes((m.bit_length() + 7) // 8, "big").decode())
