# cub3-clone — writeup

## Summary

Reverse the JS solver to compute a valid solved-cube state, then POST to `/solve`.

## Steps

1. Browse the challenge URL; inspect bundled JS (`/static/cube.js`).
2. Identify the cube state representation and the `validate()` function.
3. Either run the engine in a headless browser to reach a solved state, or compute the move sequence and POST it.
4. POST to `/solve` with the solved state → flag.

## Flag

`mctf{k3y5tr0k35_b4tt13_w0n}`
