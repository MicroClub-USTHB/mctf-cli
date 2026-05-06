# rsa-baby — writeup

## Summary

Textbook RSA with a small modulus. Factor `n`, recover `d`, decrypt.

## Steps

1. The handout `transcript.txt` provides `(n, e, c)` where `n` is 64 bits.
2. Factor `n` (any of `factordb.com`, `sympy.factorint`, or `msieve` works in seconds).
3. Compute `d = e^-1 mod (p-1)(q-1)`.
4. Decrypt: `m = c^d mod n`. Convert long-int → bytes.
5. The plaintext is the flag.

## Flag

`mctf{f4ct0r1ng_15_h4rd_unt1l_1t_15nt}`

## Time to solve (intended)

~10 minutes for someone who's seen RSA once.
