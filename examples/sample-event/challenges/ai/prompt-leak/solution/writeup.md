# Solution: prompt-leak

The asset `instructions.txt` reveals a confidential system prompt with a base64 encoded token:

```
System token (confidential): bWN0ZntwcjBtcHRfMW5qM2N0MTBuX3MzY3IzdF9sMzRrfQ==
```

Decode it:

```bash
echo "bWN0ZntwcjBtcHRfMW5qM2N0MTBuX3MzY3IzdF9sMzRrfQ==" | base64 -d
```

Output: `mctf{pr0mpt_1nj3ct10n_s3cr3t_l34k}`
