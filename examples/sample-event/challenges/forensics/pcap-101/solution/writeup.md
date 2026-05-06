# pcap-101 — writeup

## Summary

Filter HTTP traffic, find the suspicious POST, base64-decode the body.

## Steps

1. Open `capture.pcapng` in Wireshark.
2. Apply display filter: `http.request.method == POST`.
3. Follow TCP stream on the `/upload` request.
4. The body is base64-encoded; decode → flag.

## Flag

`mctf{w1r3sh4rk_15_y0ur_fr13nd}`
