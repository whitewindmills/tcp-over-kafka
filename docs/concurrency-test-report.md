# tcp-over-kafka Concurrency Test Report

## Summary

I ran a mixed SSH and HTTPS concurrency sweep against the deployed `tcp-over-kafka` SOCKS5 proxy.

Result:
- Baseline `1 SSH + 1 HTTPS` passed repeatedly.
- Standard concurrency `5 SSH + 5 HTTPS` passed.
- Incremental loads `6+6`, `7+7`, `8+8`, and `9+9` all passed.
- The first sustained failure appeared at `10 SSH + 10 HTTPS`.
- Recommended maximum concurrency level: `18` total concurrent sessions, split as `9 SSH + 9 HTTPS`.

## Environment

- Client host: `10.253.15.167`
- Relay host: `10.253.15.168`
- Broker host: `10.253.15.166:9092`
- Client service: `tcp-over-kafka-client.service`
- Server service: `tcp-over-kafka-server.service`
- HTTPS test service: `hello-world-https.service`
- SSH verification used the built-in `tcp-over-kafka proxy` subcommand.
- HTTPS verification used `curl --socks5-hostname` with the deployed CA certificate.

Service health during testing:
- `tcp-over-kafka-client.service`: active
- `tcp-over-kafka-server.service`: active
- `hello-world-https.service`: active

## Method

I executed concurrent SSH and HTTPS requests through the SOCKS5 listener on `127.0.0.1:1234` from the client host.

Commands used:
- SSH:
  - `ssh -o ProxyCommand='/usr/local/bin/tcp-over-kafka proxy --socks 127.0.0.1:1234 %h %p' root@10.253.15.168 hostname`
- HTTPS:
  - `curl --cacert /root/poc/certs/hello-world-ca.pem --socks5-hostname 127.0.0.1:1234 https://10.253.15.168/`

Load profile:
- Baseline: `1 SSH + 1 HTTPS`, repeated 3 times
- Standard concurrency: `5 SSH + 5 HTTPS`
- Graduated load: `6 + 6`, `7 + 7`, `8 + 8`, `9 + 9`, then `10 + 10`

## Results

| Load | Result | Notes |
| --- | --- | --- |
| 1 SSH + 1 HTTPS | Pass | Repeated 3 times successfully |
| 5 SSH + 5 HTTPS | Pass | 10/10 successful |
| 6 SSH + 6 HTTPS | Pass | 12/12 successful |
| 7 SSH + 7 HTTPS | Pass | 14/14 successful |
| 8 SSH + 8 HTTPS | Pass | 16/16 successful |
| 9 SSH + 9 HTTPS | Pass | 18/18 successful |
| 10 SSH + 10 HTTPS | Partial fail | 19/20 successful, 1 HTTPS timeout |

Observed timing:
- `1 + 1`: about `0.21s` to `0.45s` per pair
- `5 + 5`: about `0.40s` total elapsed
- `6 + 6`: about `0.47s` total elapsed
- `7 + 7`: about `0.53s` total elapsed
- `8 + 8`: about `0.63s` total elapsed
- `9 + 9`: about `0.71s` total elapsed
- `10 + 10`: about `3.78s` total elapsed, with one HTTPS failure

Failure detail at `10 + 10`:
- `curl: (97) Can't complete SOCKS5 connection to 10.253.15.168. (1)`
- Relay-side log showed:
  - `open session failed: dial target 10.253.15.168:443: connect: connection timed out`

## Recommended Maximum Concurrency

Recommended maximum concurrency level:
- `18` total concurrent sessions
- `9` SSH sessions + `9` HTTPS sessions

Rationale:
- All requests succeeded at that level.
- The next step, `10 + 10`, introduced the first reproducible failure.
- This gives a practical ceiling for the current POC while preserving stable behavior.

## Notes

- The tunnel remained up during the sweep.
- Direct east-west TCP between `10.253.15.167` and `10.253.15.168` remained blocked by firewall policy.
- The failure at higher load was limited to the HTTPS side and did not indicate a service restart.
- This is a POC-scale concurrency ceiling, not a hard architectural limit.
