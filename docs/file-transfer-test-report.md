# tcp-over-kafka File Transfer Test Report

## Summary

I verified file transfer over the deployed `tcp-over-kafka` SSH proxy using `scp` through the built-in `proxy` subcommand, then expanded the same test into a progressive file-size sweep.

Result:
- A 1 MiB random file uploaded successfully from the client host to the relay host.
- The relay-host checksum matched the source checksum.
- The file downloaded back successfully to the client host.
- The downloaded copy checksum matched the original checksum.
- File sizes `1 MiB`, `4 MiB`, `16 MiB`, `64 MiB`, `256 MiB`, and `512 MiB` all succeeded.
- The largest tested file size, `512 MiB`, also completed a round-trip download with a matching checksum.

Recommended file size limit for the current POC:
- `512 MiB` per transfer, tested and verified

## Environment

- Client host: `10.253.15.167`
- Relay host: `10.253.15.168`
- Proxy path: `tcp-over-kafka proxy --socks 127.0.0.1:1234`
- SSH identity used for transfer: `/root/poc/ssh/id_ed25519`
- Client service: `tcp-over-kafka-client.service`
- Server service: `tcp-over-kafka-server.service`

Service health during testing:
- `tcp-over-kafka-client.service`: active
- `tcp-over-kafka-server.service`: active

## Method

The transfer test used `scp` over the SSH ProxyCommand path to move a binary payload between the client and relay hosts.

Test file:
- Content: random data from `/dev/urandom`
- Sweep sizes: `1 MiB`, `4 MiB`, `16 MiB`, `64 MiB`, `256 MiB`, `512 MiB`

Transfer flow:
1. Upload the file from `10.253.15.167` to `10.253.15.168`.
2. Verify the SHA-256 digest on both sides.
3. Download the file back from `10.253.15.168` to `10.253.15.167`.
4. Verify the return-transfer digest matches the original.

## Results

Upload checksum for the initial 1 MiB transfer:
- `b2ad438cc2f1f059c9e75f08a9c492c4113dd746f340b10da432cdf23a723632`

Round-trip verification at `512 MiB`:
- Original checksum: `9acca8e8c22201155389f65abbf6bc9723edc7384ead80503839f49dcc56d767`
- Downloaded checksum: `9acca8e8c22201155389f65abbf6bc9723edc7384ead80503839f49dcc56d767`
- Match: `true`

Size sweep timing:

| Size | Upload Result | Elapsed |
| --- | --- | --- |
| 1 MiB | Pass | 0.470s |
| 4 MiB | Pass | 0.536s |
| 16 MiB | Pass | 0.945s |
| 64 MiB | Pass | 2.391s |
| 256 MiB | Pass | 8.397s |
| 512 MiB | Pass | 15.643s |

## Recommendation

Recommended file size limit for routine use:
- `512 MiB` per transfer

Notes:
- This is the largest size validated in the current environment.
- No failure was observed through `512 MiB`, so this is a tested recommendation rather than a hard protocol ceiling.
- If you need a more conservative operational target, `256 MiB` is also well within the measured stable range.

## Conclusion

The file transfer path is working end-to-end in both directions through the `tcp-over-kafka` SSH proxy, and the Kafka relay preserved file contents across both upload and download at the largest tested size.
